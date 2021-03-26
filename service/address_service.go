package service

import (
	"context"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"
	venustypes "github.com/filecoin-project/venus/pkg/types"
	"github.com/sirupsen/logrus"
	"golang.org/x/xerrors"

	"github.com/ipfs-force-community/venus-messager/config"
	"github.com/ipfs-force-community/venus-messager/models/repo"
	"github.com/ipfs-force-community/venus-messager/types"
)

type AddressService struct {
	repo repo.Repo
	log  *logrus.Logger

	walletService *WalletService
	nodeClient    *NodeClient
	cfg           *config.AddressConfig

	addrInfo  map[address.Address]*AddressInfo
	amendAddr chan address.Address
	l         sync.Mutex
}

type AddressInfo struct {
	State        types.AddressState
	WalletId     types.UUID
	SelectMsgNum int
	WalletClient IWalletClient
}

func NewAddressService(repo repo.Repo, logger *logrus.Logger, walletService *WalletService, nodeClient *NodeClient, cfg *config.AddressConfig) (*AddressService, error) {
	addressService := &AddressService{
		repo:          repo,
		log:           logger,
		walletService: walletService,
		nodeClient:    nodeClient,
		cfg:           cfg,
		addrInfo:      make(map[address.Address]*AddressInfo),
		amendAddr:     make(chan address.Address, 10),
	}

	if err := addressService.listenAddressChange(context.TODO()); err != nil {
		return nil, err
	}

	if err := addressService.checkAddressState(); err != nil {
		return nil, err
	}

	addressService.listenWalletDel()

	return addressService, nil
}

func (addressService *AddressService) SaveAddress(ctx context.Context, address *types.Address) (types.UUID, error) {
	return addressService.repo.AddressRepo().SaveAddress(ctx, address)
}

func (addressService *AddressService) UpdateAddress(ctx context.Context, address *types.Address) error {
	return addressService.repo.AddressRepo().UpdateAddress(ctx, address)
}

func (addressService *AddressService) UpdateNonce(ctx context.Context, addr address.Address, nonce uint64) (address.Address, error) {
	return addressService.repo.AddressRepo().UpdateNonce(ctx, addr, nonce)
}

func (addressService *AddressService) UpdateAddressState(ctx context.Context, addr address.Address, state types.AddressState) (address.Address, error) {
	return addressService.repo.AddressRepo().UpdateAddressState(ctx, addr, state)
}

func (addressService *AddressService) GetAddress(ctx context.Context, addr address.Address) (*types.Address, error) {
	return addressService.repo.AddressRepo().GetAddress(ctx, addr)
}

func (addressService *AddressService) HasAddress(ctx context.Context, addr address.Address) (bool, error) {
	return addressService.repo.AddressRepo().HasAddress(ctx, addr)
}

func (addressService *AddressService) ListAddress(ctx context.Context) ([]*types.Address, error) {
	return addressService.repo.AddressRepo().ListAddress(ctx)
}

// DeleteAddress first change the address status to frozen, confirm that all signed messages are on chain,
// and then delete the address
func (addressService *AddressService) DeleteAddress(ctx context.Context, addr address.Address) (address.Address, error) {
	_, err := addressService.repo.AddressRepo().UpdateAddressState(ctx, addr, types.Notfound)
	if err != nil {
		return address.Undef, err
	}
	addressService.setAddressState(addr, types.Notfound)

	if err := addressService.repo.MessageRepo().UpdateUnFilledMessageStateByAddress(addr, types.NoWalletMsg); err != nil {
		return address.Undef, err
	}

	go func() {
		addressService.amendAddr <- addr
	}()
	addressService.log.Infof("change address %v state to %d", addr.String(), types.Notfound)

	return addr, nil
}

func (addressService *AddressService) ForbiddenAddress(ctx context.Context, addr address.Address) (address.Address, error) {
	_, err := addressService.repo.AddressRepo().UpdateAddressState(ctx, addr, types.Forbiden)
	if err != nil {
		return address.Undef, err
	}

	addressService.setAddressState(addr, types.Forbiden)
	addressService.log.Infof("forbidden address %v", addr.String())

	return address.Undef, nil
}

func (addressService *AddressService) ActiveAddress(ctx context.Context, addr address.Address) (address.Address, error) {
	_, err := addressService.repo.AddressRepo().UpdateAddressState(ctx, addr, types.Alive)
	if err != nil {
		return address.Undef, err
	}

	addressService.setAddressState(addr, types.Alive)
	addressService.log.Infof("permit address %v", addr.String())

	return address.Undef, nil
}

func (addressService *AddressService) UpdateSelectMsgNum(ctx context.Context, addr address.Address, num int) (address.Address, error) {
	return addr, addressService.repo.AddressRepo().UpdateSelectMsgNum(ctx, addr, num)
}

func (addressService *AddressService) getLocalAddress() error {
	addrsInfo, err := addressService.ListAddress(context.Background())
	if err != nil {
		return err
	}

	for _, info := range addrsInfo {
		cli, ok := addressService.walletService.walletClients[info.WalletID]
		if !ok {
			addressService.log.Errorf("not found wallet client, uuid: %v", info.WalletID)
			continue
		}

		addressService.SetAddressInfo(info.Addr, &AddressInfo{
			State:        info.State,
			WalletId:     info.WalletID,
			SelectMsgNum: info.SelectMsgNum,
			WalletClient: cli,
		})
	}

	return nil
}

func (addressService *AddressService) listenAddressChange(ctx context.Context) error {
	if err := addressService.getLocalAddress(); err != nil {
		return xerrors.Errorf("get local address and nonce failed: %v", err)
	}
	go func() {
		ticker := time.NewTicker(time.Duration(addressService.cfg.RemoteWalletSweepInterval) * time.Second)
		for {
			select {
			case <-ticker.C:
				for walletID, cli := range addressService.walletService.walletClients {
					if err := addressService.ProcessWallet(ctx, walletID, cli); err != nil {
						addressService.log.Errorf("process wallet failed name: %s, error: %v", walletID, err)
					}
				}
			case <-ctx.Done():
				addressService.log.Warnf("context error: %v", ctx.Err())
				return
			}
		}
	}()

	return nil
}

func (addressService *AddressService) ProcessWallet(ctx context.Context, walletID types.UUID, cli IWalletClient) error {
	addrs, err := cli.WalletList(ctx)
	if err != nil {
		return xerrors.Errorf("get wallet list failed error: %v", err)
	}

	walletAddrs := addressService.ListOneWalletAddress(walletID)
	for _, addr := range addrs {
		delete(walletAddrs, addr)

		if addrInfo, ok := addressService.GetAddressInfo(addr); ok && addrInfo.State == types.Alive {
			continue
		}

		var nonce uint64
		actor, err := addressService.nodeClient.StateGetActor(context.Background(), addr, venustypes.EmptyTSK)
		if err != nil {
			addressService.log.Warnf("get actor failed, addr: %s, err: %v", addr, err)
		} else {
			nonce = actor.Nonce //current nonce should big than nonce on chain
		}

		has, err := addressService.HasAddress(ctx, addr)
		if err != nil {
			addressService.log.Errorf("found address failed %s err: %v", addr.String(), err)
			continue
		}
		ta := &types.Address{
			Addr:      addr,
			Nonce:     nonce,
			WalletID:  walletID,
			UpdatedAt: time.Now(),
			State:     types.Alive,
			IsDeleted: -1,
		}
		if !has {
			ta.ID = types.NewUUID()
			_, err = addressService.SaveAddress(context.Background(), ta)
			if err != nil {
				addressService.log.Errorf("save address failed %s err: %v", addr.String(), err)
				continue
			}
		} else {
			err = addressService.UpdateAddress(context.Background(), ta)
			if err != nil {
				addressService.log.Errorf("update address failed %s err: %v", addr.String(), err)
				continue
			}
		}

		addressService.SetAddressInfo(addr, &AddressInfo{
			State:        ta.State,
			WalletId:     ta.WalletID,
			WalletClient: cli,
		})
	}

	// address to handle remote wallet deletion
	for addr := range walletAddrs {
		addrInfo, ok := addressService.GetAddressInfo(addr)
		if !ok || addrInfo.State == types.Notfound {
			continue
		}
		addressService.log.Infof("remote wallet delete address %s", addr.String())
		if _, err := addressService.DeleteAddress(ctx, addr); err != nil {
			addressService.log.Errorf("delete address %v", err)
		}
	}

	return nil
}

func (addressService *AddressService) checkAddressState() error {
	addrList, err := addressService.ListAddress(context.TODO())
	if err != nil {
		return err
	}

	for _, addr := range addrList {
		if addr.State == types.Notfound {
			addressService.amendAddr <- addr.Addr
		}
	}

	go func() {
		for addr := range addressService.amendAddr {
			var isDeleted bool
			msgs, err := addressService.repo.MessageRepo().ListFilledMessageByAddress(addr)
			if err != nil {
				addressService.log.Errorf("get filled message %v", err)
			} else if len(msgs) == 0 {
				// add address again
				if addrInfo, err := addressService.repo.AddressRepo().GetAddress(context.TODO(), addr); err == nil && addrInfo.State == types.Alive {
					isDeleted = true
				} else if err := addressService.repo.AddressRepo().DelAddress(context.TODO(), addr); err != nil {
					addressService.log.Errorf("update address state %v", err)
				} else {
					addressService.RemoveAddressInfo(addr)
					addressService.log.Infof("delete address %v", addr.String())
					isDeleted = true
				}
			}
			if !isDeleted {
				go func() {
					time.Sleep(time.Second * 60)
					addressService.amendAddr <- addr
				}()
			}
		}
	}()

	return nil
}

func (addressService *AddressService) listenWalletDel() {
	go func() {
		for walletId := range addressService.walletService.delWalletChan {
			addrs := addressService.ListOneWalletAddress(walletId)
			for addr := range addrs {
				addressService.log.Infof("wallet %v delete address %s", walletId, addr)
				if _, err := addressService.DeleteAddress(context.TODO(), addr); err != nil {
					addressService.log.Errorf("delete address from wallet %s %s %v", walletId.String(), addr, err)
				}
			}
		}
	}()
}

/////////// address cache ///////////

func (addressService *AddressService) SetAddressInfo(addr address.Address, info *AddressInfo) {
	addressService.l.Lock()
	defer addressService.l.Unlock()

	addressService.addrInfo[addr] = info
}

func (addressService *AddressService) GetAddressInfo(addr address.Address) (*AddressInfo, bool) {
	addressService.l.Lock()
	defer addressService.l.Unlock()
	if info, ok := addressService.addrInfo[addr]; ok {
		return info, ok
	}

	return nil, false
}

func (addressService *AddressService) setAddressState(addr address.Address, state types.AddressState) {
	addressService.l.Lock()
	defer addressService.l.Unlock()
	if info, ok := addressService.addrInfo[addr]; ok {
		info.State = state
	}
}

func (addressService *AddressService) RemoveAddressInfo(addr address.Address) {
	addressService.l.Lock()
	defer addressService.l.Unlock()

	delete(addressService.addrInfo, addr)
}

func (addressService *AddressService) ListAddressInfo() map[address.Address]AddressInfo {
	addressService.l.Lock()
	defer addressService.l.Unlock()
	addrInfos := make(map[address.Address]AddressInfo, len(addressService.addrInfo))
	for addr, info := range addressService.addrInfo {
		addrInfos[addr] = *info
	}

	return addrInfos
}

func (addressService *AddressService) ListOneWalletAddress(walletId types.UUID) map[address.Address]struct{} {
	addressService.l.Lock()
	defer addressService.l.Unlock()
	addrs := make(map[address.Address]struct{})
	for addr, info := range addressService.addrInfo {
		if info.WalletId == walletId {
			addrs[addr] = struct{}{}
		}
	}

	return addrs
}

package mysql

import (
	"context"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/venus-messager/models/mtypes"

	shared "github.com/filecoin-project/venus/venus-shared/types"
	"gorm.io/gorm"

	"github.com/filecoin-project/venus-messager/models/repo"
	types "github.com/filecoin-project/venus/venus-shared/types/messager"
)

type mysqlActorCfg struct {
	ID       shared.UUID  `gorm:"column:id;type:varchar(256);primary_key;"` // 主键
	NVersion uint         `gorm:"column:n_version;type:unsigned int;NOT NULL"`
	CodeCid  mtypes.DBCid `gorm:"column:code_cid;type:varchar(256);index:idx_code_cid_method,unique;NOT NULL;"`
	Method   uint64       `gorm:"column:method;type:unsigned bigint;index:idx_code_cid_method,unique;NOT NULL"`

	SelectSpec

	CreatedAt time.Time `gorm:"column:created_at;index;NOT NULL"` // 创建时间
	UpdatedAt time.Time `gorm:"column:updated_at;index;NOT NULL"` // 更新时间
}

func fromActorCfg(actorCfg *types.ActorCfg) *mysqlActorCfg {
	return &mysqlActorCfg{
		ID:       actorCfg.ID,
		NVersion: uint(actorCfg.NVersion),
		CodeCid:  mtypes.NewDBCid(actorCfg.CodeCid),
		Method:   uint64(actorCfg.Method),
		SelectSpec: SelectSpec{
			SelMsgNum:         actorCfg.SelMsgNum,
			GasOverEstimation: actorCfg.GasOverEstimation,
			GasOverPremium:    actorCfg.GasOverPremium,
			MaxFee:            mtypes.SafeFromGo(actorCfg.MaxFee.Int),
			GasFeeCap:         mtypes.SafeFromGo(actorCfg.GasFeeCap.Int),
			BaseFee:           mtypes.SafeFromGo(actorCfg.BaseFee.Int),
		},
		CreatedAt: actorCfg.CreatedAt,
		UpdatedAt: actorCfg.UpdatedAt,
	}
}

func (mysqlActorCfg mysqlActorCfg) ActorCfg() *types.ActorCfg {
	return &types.ActorCfg{
		ID:       mysqlActorCfg.ID,
		NVersion: network.Version(mysqlActorCfg.NVersion),
		MethodType: types.MethodType{
			CodeCid: mysqlActorCfg.CodeCid.Cid(),
			Method:  abi.MethodNum(mysqlActorCfg.Method),
		},
		SelectSpec: types.SelectSpec{
			SelMsgNum:         mysqlActorCfg.SelMsgNum,
			GasOverEstimation: mysqlActorCfg.GasOverEstimation,
			GasOverPremium:    mysqlActorCfg.GasOverPremium,
			MaxFee:            big.Int(mtypes.SafeFromGo(mysqlActorCfg.MaxFee.Int)),
			GasFeeCap:         big.Int(mtypes.SafeFromGo(mysqlActorCfg.GasFeeCap.Int)),
			BaseFee:           big.Int(mtypes.SafeFromGo(mysqlActorCfg.BaseFee.Int)),
		},
		CreatedAt: mysqlActorCfg.CreatedAt,
		UpdatedAt: mysqlActorCfg.UpdatedAt,
	}
}

func (mysqlActorCfg mysqlActorCfg) TableName() string {
	return "actor_cfg"
}

var _ repo.ActorCfgRepo = (*mysqlActorCfgRepo)(nil)

type mysqlActorCfgRepo struct {
	*gorm.DB
}

func newMysqlActorCfgRepo(db *gorm.DB) *mysqlActorCfgRepo {
	return &mysqlActorCfgRepo{DB: db}
}

func (s *mysqlActorCfgRepo) SaveActorCfg(ctx context.Context, actorCfg *types.ActorCfg) error {
	return s.DB.Save(fromActorCfg(actorCfg)).Error
}

func (s *mysqlActorCfgRepo) GetActorCfgByMethodType(ctx context.Context, methodType *types.MethodType) (*types.ActorCfg, error) {
	var a mysqlActorCfg
	if err := s.DB.Take(&a, "code_cid = ? and method = ?", methodType.CodeCid.String(), methodType.Method).Error; err != nil {
		return nil, err
	}

	return a.ActorCfg(), nil
}

func (s *mysqlActorCfgRepo) GetActorCfgByID(ctx context.Context, id shared.UUID) (*types.ActorCfg, error) {
	var a mysqlActorCfg
	if err := s.DB.Take(&a, "id = ?", id).Error; err != nil {
		return nil, err
	}

	return a.ActorCfg(), nil
}

func (s *mysqlActorCfgRepo) ListActorCfg(ctx context.Context) ([]*types.ActorCfg, error) {
	var list []*mysqlActorCfg
	if err := s.DB.Find(&list).Error; err != nil {
		return nil, err
	}

	result := make([]*types.ActorCfg, len(list))
	for index, r := range list {
		result[index] = r.ActorCfg()
	}

	return result, nil
}

func (s *mysqlActorCfgRepo) DelActorCfgByMethodType(ctx context.Context, methodType *types.MethodType) error {
	return s.DB.Delete(mysqlActorCfg{}, "code_cid = ? and method = ?", methodType.CodeCid.String(), methodType.Method).Error
}

func (s *mysqlActorCfgRepo) DelActorCfgById(ctx context.Context, id shared.UUID) error {
	return s.DB.Delete(mysqlActorCfg{}, "id = ?", id).Error
}

func (s *mysqlActorCfgRepo) UpdateSelectSpecById(ctx context.Context, id shared.UUID, spec *types.ChangeSelectSpecParams) error {
	updateColumns := make(map[string]interface{}, 6)
	if !spec.GasFeeCap.Nil() {
		updateColumns["gas_fee_cap"] = spec.GasFeeCap.String()
	}
	if !spec.BaseFee.Nil() {
		updateColumns["base_fee"] = spec.BaseFee.String()
	}
	if !spec.MaxFee.Nil() {
		updateColumns["max_fee"] = spec.MaxFee.String()
	}

	if spec.SelMsgNum != nil {
		updateColumns["sel_msg_num"] = *spec.SelMsgNum
	}
	if spec.GasOverEstimation != nil {
		updateColumns["gas_over_estimation"] = *spec.GasOverEstimation
	}
	if spec.GasOverPremium != nil {
		updateColumns["gas_over_premium"] = *spec.GasOverPremium
	}

	if len(updateColumns) == 0 {
		return nil
	}

	updateColumns["updated_at"] = time.Now()

	return s.DB.Model((*mysqlActorCfg)(nil)).Where("id = ?", id).UpdateColumns(updateColumns).Error
}

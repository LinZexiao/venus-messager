package sqlite

import (
	"context"

	"github.com/filecoin-project/go-state-types/big"

	"gorm.io/gorm"

	"github.com/filecoin-project/venus-messager/models/mtypes"
	"github.com/filecoin-project/venus-messager/models/repo"
	types "github.com/filecoin-project/venus/venus-shared/types/messager"
)

type sqliteSharedParams struct {
	ID uint `gorm:"primary_key;column:id;type:INT unsigned AUTO_INCREMENT;NOT NULL" json:"id"`

	GasOverEstimation float64    `gorm:"column:gas_over_estimation;type:REAL;NOT NULL"`
	MaxFee            mtypes.Int `gorm:"column:max_fee;type:varchar(256);NOT NULL;default:0"`
	GasFeeCap         mtypes.Int `gorm:"column:gas_fee_cap;type:varchar(256);NOT NULL;default:0"`
	GasOverPremium    float64    `gorm:"column:gas_over_premium;type:REAL;NOT NULL;default:0"`
	BaseFee           mtypes.Int `gorm:"column:base_fee;type:varchar(256);default:0"`

	SelMsgNum uint64 `gorm:"column:sel_msg_num;type:UNSIGNED BIG INT;NOT NULL"`
}

func fromSharedParams(sp types.SharedSpec) *sqliteSharedParams {
	return &sqliteSharedParams{
		ID:                sp.ID,
		GasOverEstimation: sp.GasOverEstimation,
		MaxFee:            mtypes.SafeFromGo(sp.MaxFee.Int),
		GasFeeCap:         mtypes.SafeFromGo(sp.GasFeeCap.Int),
		BaseFee:           mtypes.SafeFromGo(sp.BaseFee.Int),
		GasOverPremium:    sp.GasOverPremium,
		SelMsgNum:         sp.SelMsgNum,
	}
}

func (ssp sqliteSharedParams) SharedParams() *types.SharedSpec {
	return &types.SharedSpec{
		ID:                ssp.ID,
		GasOverEstimation: ssp.GasOverEstimation,
		MaxFee:            big.Int(mtypes.SafeFromGo(ssp.MaxFee.Int)),
		GasFeeCap:         big.Int(mtypes.SafeFromGo(ssp.GasFeeCap.Int)),
		BaseFee:           big.Int(mtypes.SafeFromGo(ssp.BaseFee.Int)),
		GasOverPremium:    ssp.GasOverPremium,
		SelMsgNum:         ssp.SelMsgNum,
	}
}

func (ssp sqliteSharedParams) TableName() string {
	return "shared_params"
}

var _ repo.SharedParamsRepo = (*sqliteSharedParamsRepo)(nil)

type sqliteSharedParamsRepo struct {
	*gorm.DB
}

func newSqliteSharedParamsRepo(db *gorm.DB) sqliteSharedParamsRepo {
	return sqliteSharedParamsRepo{DB: db}
}

func (s sqliteSharedParamsRepo) GetSharedParams(ctx context.Context) (*types.SharedSpec, error) {
	var ssp sqliteSharedParams
	if err := s.DB.Take(&ssp).Error; err != nil {
		return nil, err
	}
	return ssp.SharedParams(), nil
}

func (s sqliteSharedParamsRepo) SetSharedParams(ctx context.Context, params *types.SharedSpec) (uint, error) {
	var ssp sqliteSharedParams
	// make sure ID is 1
	params.ID = 1
	if err := s.DB.Where("id = ?", 1).Take(&ssp).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := s.DB.Save(fromSharedParams(*params)).Error; err != nil {
				return 0, err
			}
			return params.ID, nil
		}
		return 0, err
	}

	if err := s.DB.Save(fromSharedParams(*params)).Error; err != nil {
		return 0, err
	}

	return params.ID, nil
}

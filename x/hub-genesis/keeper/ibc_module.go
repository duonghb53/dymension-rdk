package keeper

import (
	"encoding/json"
	"errors"
	"time"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	transfertypes "github.com/cosmos/ibc-go/v6/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	porttypes "github.com/cosmos/ibc-go/v6/modules/core/05-port/types"
	"github.com/dymensionxyz/dymension-rdk/x/hub-genesis/types"
)

type OnChanOpenConfirmInterceptor struct {
	porttypes.IBCModule
	transfer Transfer
	k        Keeper
	getDenom GetDenomMetaData
}

type (
	Transfer         func(ctx sdk.Context, transfer *transfertypes.MsgTransfer) error
	GetDenomMetaData func(ctx sdk.Context, denom string) (banktypes.Metadata, bool)
)

func NewOnChanOpenConfirmInterceptor(next porttypes.IBCModule, t Transfer, k Keeper, d GetDenomMetaData) *OnChanOpenConfirmInterceptor {
	return &OnChanOpenConfirmInterceptor{next, t, k, d}
}

// OnChanOpenConfirm .. TODO: write a note about exactly-once ness
func (c OnChanOpenConfirmInterceptor) OnChanOpenConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	l := ctx.Logger().With("name", "hubgenesis OnChanOpenConfirm middleware", "port id", portID, "channelID", channelID)

	err := c.IBCModule.OnChanOpenConfirm(ctx, portID, channelID)
	if err != nil {
		l.Error("Next middleware.", "err", err)
		return err
	}

	if !isHub(ctx, portID, channelID) {
		return nil
	}

	l.Debug("Minting coins and sending transfers.")

	c.k.mintCoins(ctx)

	state := c.k.GetState(ctx)

	srcAccount := c.k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	srcAddr := srcAccount.GetAddress().String()

	var errs []error

	for i, a := range state.GetGenesisAccounts() {

		// NOTE: for simplicity we don't optimize to avoid sending duplicate metadata
		// we assume the hub will deduplicate
		memo, err := c.createMemo(ctx, a.Amount.Denom, i, len(state.GetGenesisAccounts()))
		if err != nil {
			err = errorsmod.Wrapf(err, "create memo: coin: %s", a.Amount)
			errs = append(errs, err)
			continue
		}

		m := transfertypes.MsgTransfer{
			SourcePort:       portID,
			SourceChannel:    channelID,
			Token:            a.Amount,
			Sender:           srcAddr,
			Receiver:         a.GetAddress(),
			TimeoutHeight:    clienttypes.Height{},
			TimeoutTimestamp: uint64(ctx.BlockTime().Add(time.Hour * 24).UnixNano()), // TODO: value?
			Memo:             memo,
		}

		err = c.transfer(ctx, &m)
		if err != nil {
			err = errorsmod.Wrapf(err, "transfer: receiver: %s: amt: %s", a.GetAddress(), a.Amount)
			errs = append(errs, err)
			continue
		}

		l.Debug("Sent genesis transfer.", "index", i, "receiver", a.GetAddress(), "amt", a.Amount)
	}

	err = errors.Join(err)
	if err != nil {
		l.Error("Genesis transfers.", "err", err) // TODO: don't log(?)
	} else {
		l.Info("Sent genesis transfers.")
	}

	return err
}

func isHub(ctx sdk.Context, portID, channelID string) bool {
	// TODO:
	return true
}

// createMemo creates a memo to go with the transfer. It's used by the hub to confirm
// that the transfer originated from the chain itself, rather than a user of the chain.
// It may also contain token metadata.
func (c OnChanOpenConfirmInterceptor) createMemo(ctx sdk.Context, denom string, i, n int) (string, error) {
	d, ok := c.getDenom(ctx, denom)
	if !ok {
		return "", errorsmod.Wrap(sdkerrors.ErrNotFound, "get denom metadata")
	}

	m := memo{}
	m.Data.Denom = d
	m.Data.TotalNumTransfers = uint64(n)
	m.Data.ThisTransferIx = uint64(i)

	bz, err := json.Marshal(m)
	if err != nil {
		return "", sdkerrors.ErrJSONMarshal
	}

	return string(bz), nil
}

type memo struct {
	Data struct {
		Denom banktypes.Metadata `json:"denom"`
		// How many transfers in total will be sent in the transfer genesis period
		TotalNumTransfers uint64 `json:"total_num_transfers"`
		// Which transfer is this? If there are 5 transfers total, they will be numbered 0,1,2,3,4.
		ThisTransferIx uint64 `json:"this_transfer_ix"`
	} `json:"genesis_transfer"`
}

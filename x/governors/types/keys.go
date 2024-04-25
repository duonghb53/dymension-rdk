package types

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	"github.com/cosmos/cosmos-sdk/types/kv"
)

const (
	// ModuleName is the name of the staking module
	ModuleName = "governors"

	// StoreKey is the string store representation
	StoreKey = "rdkgovernors"

	// QuerierRoute is the querier route for the staking module
	QuerierRoute = ModuleName

	// RouterKey is the msg router key for the staking module
	RouterKey = ModuleName
)

var (
	// Keys for store prefixes
	// Last* values are constant during a block.
	LastGovernorPowerKey = []byte{0x11} // prefix for each key to a governor index, for bonded governors
	LastTotalPowerKey    = []byte{0x12} // prefix for the total power

	GovernorsKey             = []byte{0x21} // prefix for each key to a governor
	GovernorsByConsAddrKey   = []byte{0x22} // prefix for each key to a governor index, by pubkey
	GovernorsByPowerIndexKey = []byte{0x23} // prefix for each key to a governor index, sorted by power

	DelegationKey                    = []byte{0x31} // key for a delegation
	UnbondingDelegationKey           = []byte{0x32} // key for an unbonding-delegation
	UnbondingDelegationByValIndexKey = []byte{0x33} // prefix for each key for an unbonding-delegation, by governor operator
	RedelegationKey                  = []byte{0x34} // key for a redelegation
	RedelegationByValSrcIndexKey     = []byte{0x35} // prefix for each key for an redelegation, by source governor operator
	RedelegationByValDstIndexKey     = []byte{0x36} // prefix for each key for an redelegation, by destination governor operator

	UnbondingQueueKey    = []byte{0x41} // prefix for the timestamps in unbonding queue
	RedelegationQueueKey = []byte{0x42} // prefix for the timestamps in redelegations queue
	GovernorQueueKey     = []byte{0x43} // prefix for the timestamps in governor queue

	HistoricalInfoKey = []byte{0x50} // prefix for the historical info
)

// GetGovernorKey creates the key for the governor with address
// VALUE: staking/Governor
func GetGovernorKey(operatorAddr sdk.ValAddress) []byte {
	return append(GovernorsKey, address.MustLengthPrefix(operatorAddr)...)
}

// AddressFromGovernorsKey creates the governor operator address from GovernorsKey
func AddressFromGovernorsKey(key []byte) []byte {
	kv.AssertKeyAtLeastLength(key, 3)
	return key[2:] // remove prefix bytes and address length
}

// AddressFromLastGovernorPowerKey creates the governor operator address from LastGovernorPowerKey
func AddressFromLastGovernorPowerKey(key []byte) []byte {
	kv.AssertKeyAtLeastLength(key, 3)
	return key[2:] // remove prefix bytes and address length
}

// GetGovernorsByPowerIndexKey creates the governor by power index.
// Power index is the key used in the power-store, and represents the relative
// power ranking of the governor.
// VALUE: governor operator address ([]byte)
func GetGovernorsByPowerIndexKey(governor Governor, powerReduction math.Int) []byte {
	// NOTE the address doesn't need to be stored because counter bytes must always be different
	// NOTE the larger values are of higher value

	consensusPower := sdk.TokensToConsensusPower(governor.Tokens, powerReduction)
	consensusPowerBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(consensusPowerBytes, uint64(consensusPower))

	powerBytes := consensusPowerBytes
	powerBytesLen := len(powerBytes) // 8

	addr, err := sdk.ValAddressFromBech32(governor.OperatorAddress)
	if err != nil {
		panic(err)
	}
	operAddrInvr := sdk.CopyBytes(addr)
	addrLen := len(operAddrInvr)

	for i, b := range operAddrInvr {
		operAddrInvr[i] = ^b
	}

	// key is of format prefix || powerbytes || addrLen (1byte) || addrBytes
	key := make([]byte, 1+powerBytesLen+1+addrLen)

	key[0] = GovernorsByPowerIndexKey[0]
	copy(key[1:powerBytesLen+1], powerBytes)
	key[powerBytesLen+1] = byte(addrLen)
	copy(key[powerBytesLen+2:], operAddrInvr)

	return key
}

// GetLastGovernorPowerKey creates the bonded governor index key for an operator address
func GetLastGovernorPowerKey(operator sdk.ValAddress) []byte {
	return append(LastGovernorPowerKey, address.MustLengthPrefix(operator)...)
}

// ParseGovernorPowerRankKey parses the governors operator address from power rank key
func ParseGovernorPowerRankKey(key []byte) (operAddr []byte) {
	powerBytesLen := 8

	// key is of format prefix (1 byte) || powerbytes || addrLen (1byte) || addrBytes
	operAddr = sdk.CopyBytes(key[powerBytesLen+2:])

	for i, b := range operAddr {
		operAddr[i] = ^b
	}

	return operAddr
}

// GetGovernorQueueKey returns the prefix key used for getting a set of unbonding
// governors whose unbonding completion occurs at the given time and height.
func GetGovernorQueueKey(timestamp time.Time, height int64) []byte {
	heightBz := sdk.Uint64ToBigEndian(uint64(height))
	timeBz := sdk.FormatTimeBytes(timestamp)
	timeBzL := len(timeBz)
	prefixL := len(GovernorQueueKey)

	bz := make([]byte, prefixL+8+timeBzL+8)

	// copy the prefix
	copy(bz[:prefixL], GovernorQueueKey)

	// copy the encoded time bytes length
	copy(bz[prefixL:prefixL+8], sdk.Uint64ToBigEndian(uint64(timeBzL)))

	// copy the encoded time bytes
	copy(bz[prefixL+8:prefixL+8+timeBzL], timeBz)

	// copy the encoded height
	copy(bz[prefixL+8+timeBzL:], heightBz)

	return bz
}

// ParseGovernorQueueKey returns the encoded time and height from a key created
// from GetGovernorQueueKey.
func ParseGovernorQueueKey(bz []byte) (time.Time, int64, error) {
	prefixL := len(GovernorQueueKey)
	if prefix := bz[:prefixL]; !bytes.Equal(prefix, GovernorQueueKey) {
		return time.Time{}, 0, fmt.Errorf("invalid prefix; expected: %X, got: %X", GovernorQueueKey, prefix)
	}

	timeBzL := sdk.BigEndianToUint64(bz[prefixL : prefixL+8])
	ts, err := sdk.ParseTimeBytes(bz[prefixL+8 : prefixL+8+int(timeBzL)])
	if err != nil {
		return time.Time{}, 0, err
	}

	height := sdk.BigEndianToUint64(bz[prefixL+8+int(timeBzL):])

	return ts, int64(height), nil
}

// GetDelegationKey creates the key for delegator bond with governor
// VALUE: staking/Delegation
func GetDelegationKey(delAddr sdk.AccAddress, valAddr sdk.ValAddress) []byte {
	return append(GetDelegationsKey(delAddr), address.MustLengthPrefix(valAddr)...)
}

// GetDelegationsKey creates the prefix for a delegator for all governors
func GetDelegationsKey(delAddr sdk.AccAddress) []byte {
	return append(DelegationKey, address.MustLengthPrefix(delAddr)...)
}

// GetUBDKey creates the key for an unbonding delegation by delegator and governor addr
// VALUE: staking/UnbondingDelegation
func GetUBDKey(delAddr sdk.AccAddress, valAddr sdk.ValAddress) []byte {
	return append(GetUBDsKey(delAddr.Bytes()), address.MustLengthPrefix(valAddr)...)
}

// GetUBDByValIndexKey creates the index-key for an unbonding delegation, stored by governor-index
// VALUE: none (key rearrangement used)
func GetUBDByValIndexKey(delAddr sdk.AccAddress, valAddr sdk.ValAddress) []byte {
	return append(GetUBDsByValIndexKey(valAddr), address.MustLengthPrefix(delAddr)...)
}

// GetUBDKeyFromValIndexKey rearranges the ValIndexKey to get the UBDKey
func GetUBDKeyFromValIndexKey(indexKey []byte) []byte {
	kv.AssertKeyAtLeastLength(indexKey, 2)
	addrs := indexKey[1:] // remove prefix bytes

	valAddrLen := addrs[0]
	kv.AssertKeyAtLeastLength(addrs, 2+int(valAddrLen))
	valAddr := addrs[1 : 1+valAddrLen]
	kv.AssertKeyAtLeastLength(addrs, 3+int(valAddrLen))
	delAddr := addrs[valAddrLen+2:]

	return GetUBDKey(delAddr, valAddr)
}

// GetUBDsKey creates the prefix for all unbonding delegations from a delegator
func GetUBDsKey(delAddr sdk.AccAddress) []byte {
	return append(UnbondingDelegationKey, address.MustLengthPrefix(delAddr)...)
}

// GetUBDsByValIndexKey creates the prefix keyspace for the indexes of unbonding delegations for a governor
func GetUBDsByValIndexKey(valAddr sdk.ValAddress) []byte {
	return append(UnbondingDelegationByValIndexKey, address.MustLengthPrefix(valAddr)...)
}

// GetUnbondingDelegationTimeKey creates the prefix for all unbonding delegations from a delegator
func GetUnbondingDelegationTimeKey(timestamp time.Time) []byte {
	bz := sdk.FormatTimeBytes(timestamp)
	return append(UnbondingQueueKey, bz...)
}

// GetREDKey returns a key prefix for indexing a redelegation from a delegator
// and source governor to a destination governor.
func GetREDKey(delAddr sdk.AccAddress, valSrcAddr, valDstAddr sdk.ValAddress) []byte {
	// key is of the form GetREDsKey || valSrcAddrLen (1 byte) || valSrcAddr || valDstAddrLen (1 byte) || valDstAddr
	key := make([]byte, 1+3+len(delAddr)+len(valSrcAddr)+len(valDstAddr))

	copy(key[0:2+len(delAddr)], GetREDsKey(delAddr.Bytes()))
	key[2+len(delAddr)] = byte(len(valSrcAddr))
	copy(key[3+len(delAddr):3+len(delAddr)+len(valSrcAddr)], valSrcAddr.Bytes())
	key[3+len(delAddr)+len(valSrcAddr)] = byte(len(valDstAddr))
	copy(key[4+len(delAddr)+len(valSrcAddr):], valDstAddr.Bytes())

	return key
}

// GetREDByValSrcIndexKey creates the index-key for a redelegation, stored by source-governor-index
// VALUE: none (key rearrangement used)
func GetREDByValSrcIndexKey(delAddr sdk.AccAddress, valSrcAddr, valDstAddr sdk.ValAddress) []byte {
	REDSFromValsSrcKey := GetREDsFromValSrcIndexKey(valSrcAddr)
	offset := len(REDSFromValsSrcKey)

	// key is of the form REDSFromValsSrcKey || delAddrLen (1 byte) || delAddr || valDstAddrLen (1 byte) || valDstAddr
	key := make([]byte, offset+2+len(delAddr)+len(valDstAddr))
	copy(key[0:offset], REDSFromValsSrcKey)
	key[offset] = byte(len(delAddr))
	copy(key[offset+1:offset+1+len(delAddr)], delAddr.Bytes())
	key[offset+1+len(delAddr)] = byte(len(valDstAddr))
	copy(key[offset+2+len(delAddr):], valDstAddr.Bytes())

	return key
}

// GetREDByValDstIndexKey creates the index-key for a redelegation, stored by destination-governor-index
// VALUE: none (key rearrangement used)
func GetREDByValDstIndexKey(delAddr sdk.AccAddress, valSrcAddr, valDstAddr sdk.ValAddress) []byte {
	REDSToValsDstKey := GetREDsToValDstIndexKey(valDstAddr)
	offset := len(REDSToValsDstKey)

	// key is of the form REDSToValsDstKey || delAddrLen (1 byte) || delAddr || valSrcAddrLen (1 byte) || valSrcAddr
	key := make([]byte, offset+2+len(delAddr)+len(valSrcAddr))
	copy(key[0:offset], REDSToValsDstKey)
	key[offset] = byte(len(delAddr))
	copy(key[offset+1:offset+1+len(delAddr)], delAddr.Bytes())
	key[offset+1+len(delAddr)] = byte(len(valSrcAddr))
	copy(key[offset+2+len(delAddr):], valSrcAddr.Bytes())

	return key
}

// GetREDKeyFromValSrcIndexKey rearranges the ValSrcIndexKey to get the REDKey
func GetREDKeyFromValSrcIndexKey(indexKey []byte) []byte {
	// note that first byte is prefix byte, which we remove
	kv.AssertKeyAtLeastLength(indexKey, 2)
	addrs := indexKey[1:]

	valSrcAddrLen := addrs[0]
	kv.AssertKeyAtLeastLength(addrs, int(valSrcAddrLen)+2)
	valSrcAddr := addrs[1 : valSrcAddrLen+1]
	delAddrLen := addrs[valSrcAddrLen+1]
	kv.AssertKeyAtLeastLength(addrs, int(valSrcAddrLen)+int(delAddrLen)+2)
	delAddr := addrs[valSrcAddrLen+2 : valSrcAddrLen+2+delAddrLen]
	kv.AssertKeyAtLeastLength(addrs, int(valSrcAddrLen)+int(delAddrLen)+4)
	valDstAddr := addrs[valSrcAddrLen+delAddrLen+3:]

	return GetREDKey(delAddr, valSrcAddr, valDstAddr)
}

// GetREDKeyFromValDstIndexKey rearranges the ValDstIndexKey to get the REDKey
func GetREDKeyFromValDstIndexKey(indexKey []byte) []byte {
	// note that first byte is prefix byte, which we remove
	kv.AssertKeyAtLeastLength(indexKey, 2)
	addrs := indexKey[1:]

	valDstAddrLen := addrs[0]
	kv.AssertKeyAtLeastLength(addrs, int(valDstAddrLen)+2)
	valDstAddr := addrs[1 : valDstAddrLen+1]
	delAddrLen := addrs[valDstAddrLen+1]
	kv.AssertKeyAtLeastLength(addrs, int(valDstAddrLen)+int(delAddrLen)+3)
	delAddr := addrs[valDstAddrLen+2 : valDstAddrLen+2+delAddrLen]
	kv.AssertKeyAtLeastLength(addrs, int(valDstAddrLen)+int(delAddrLen)+4)
	valSrcAddr := addrs[valDstAddrLen+delAddrLen+3:]

	return GetREDKey(delAddr, valSrcAddr, valDstAddr)
}

// GetRedelegationTimeKey returns a key prefix for indexing an unbonding
// redelegation based on a completion time.
func GetRedelegationTimeKey(timestamp time.Time) []byte {
	bz := sdk.FormatTimeBytes(timestamp)
	return append(RedelegationQueueKey, bz...)
}

// GetREDsKey returns a key prefix for indexing a redelegation from a delegator
// address.
func GetREDsKey(delAddr sdk.AccAddress) []byte {
	return append(RedelegationKey, address.MustLengthPrefix(delAddr)...)
}

// GetREDsFromValSrcIndexKey returns a key prefix for indexing a redelegation to
// a source governor.
func GetREDsFromValSrcIndexKey(valSrcAddr sdk.ValAddress) []byte {
	return append(RedelegationByValSrcIndexKey, address.MustLengthPrefix(valSrcAddr)...)
}

// GetREDsToValDstIndexKey returns a key prefix for indexing a redelegation to a
// destination (target) governor.
func GetREDsToValDstIndexKey(valDstAddr sdk.ValAddress) []byte {
	return append(RedelegationByValDstIndexKey, address.MustLengthPrefix(valDstAddr)...)
}

// GetREDsByDelToValDstIndexKey returns a key prefix for indexing a redelegation
// from an address to a source governor.
func GetREDsByDelToValDstIndexKey(delAddr sdk.AccAddress, valDstAddr sdk.ValAddress) []byte {
	return append(GetREDsToValDstIndexKey(valDstAddr), address.MustLengthPrefix(delAddr)...)
}

// GetHistoricalInfoKey returns a key prefix for indexing HistoricalInfo objects.
func GetHistoricalInfoKey(height int64) []byte {
	return append(HistoricalInfoKey, []byte(strconv.FormatInt(height, 10))...)
}
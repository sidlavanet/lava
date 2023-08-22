package utils

import (
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strings"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/lavanet/lava/utils/decoder"
	"github.com/lavanet/lava/x/plans/types"
	"github.com/mitchellh/mapstructure"
)

type (
	PlansAddProposalJSON struct {
		Proposal types.PlansAddProposal `json:"proposal"`
		Deposit  string                 `json:"deposit" yaml:"deposit"`
	}
)

type (
	PlansDelProposalJSON struct {
		Proposal types.PlansDelProposal `json:"proposal"`
		Deposit  string                 `json:"deposit" yaml:"deposit"`
	}
)

// Parse plans add proposal JSON form file
func ParsePlansAddProposalJSON(cdc *codec.LegacyAmino, proposalFile string) (ret PlansAddProposalJSON, err error) {
	for _, fileName := range strings.Split(proposalFile, ",") {
		var proposal PlansAddProposalJSON

		decoderHooks := []mapstructure.DecodeHookFunc{
			priceDecodeHookFunc,
		}

		var (
			unused []string
			unset  []string
			err    error
		)

		err = decoder.DecodeFile(fileName, "proposal", &proposal.Proposal, decoderHooks, &unset, &unused)
		if err != nil {
			return proposal, err
		}

		err = decoder.DecodeFile(fileName, "deposit", &proposal.Deposit, nil, nil, nil)
		if err != nil {
			return proposal, err
		}

		if len(ret.Proposal.Plans) > 0 {
			ret.Proposal.Plans = append(ret.Proposal.Plans, proposal.Proposal.Plans...)
			ret.Proposal.Description = proposal.Proposal.Description + " " + ret.Proposal.Description
			ret.Proposal.Title = proposal.Proposal.Title + " " + ret.Proposal.Title
			retDeposit, err := sdk.ParseCoinNormalized(ret.Deposit)
			if err != nil {
				return proposal, err
			}
			proposalDeposit, err := sdk.ParseCoinNormalized(proposal.Deposit)
			if err != nil {
				return proposal, err
			}
			ret.Deposit = retDeposit.Add(proposalDeposit).String()
		} else {
			ret = proposal
		}
	}
	return ret, nil
}

// Parse plans delete proposal JSON form file
func ParsePlansDelProposalJSON(cdc *codec.LegacyAmino, proposalFile string) (ret PlansDelProposalJSON, err error) {
	for _, fileName := range strings.Split(proposalFile, ",") {
		var proposal PlansDelProposalJSON

		contents, err := os.ReadFile(fileName)
		if err != nil {
			return proposal, err
		}

		if err := cdc.UnmarshalJSON(contents, &proposal); err != nil {
			return proposal, err
		}
		if len(ret.Proposal.Plans) > 0 {
			ret.Proposal.Plans = append(ret.Proposal.Plans, proposal.Proposal.Plans...)
			ret.Proposal.Description = proposal.Proposal.Description + " " + ret.Proposal.Description
			ret.Proposal.Title = proposal.Proposal.Title + " " + ret.Proposal.Title
			retDeposit, err := sdk.ParseCoinNormalized(ret.Deposit)
			if err != nil {
				return proposal, err
			}
			proposalDeposit, err := sdk.ParseCoinNormalized(proposal.Deposit)
			if err != nil {
				return proposal, err
			}
			ret.Deposit = retDeposit.Add(proposalDeposit).String()
		} else {
			ret = proposal
		}
	}
	return ret, nil
}

// Plan Hook Functions

// priceDecodeHookFunc helps the decoder to correctly unmarshal the price field's amount (type sdk.Int)
func priceDecodeHookFunc(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if t == reflect.TypeOf(sdk.NewInt(0)) {
		amountStr, ok := data.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected data type for amount field")
		}

		// Convert the string amount to an sdk.Int
		amount, ok := new(big.Int).SetString(amountStr, 10)
		if !ok {
			return nil, fmt.Errorf("failed to convert amount to sdk.Int")
		}
		return sdk.NewIntFromBigInt(amount), nil
	}

	return data, nil
}

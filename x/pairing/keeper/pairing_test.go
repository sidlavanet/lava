package keeper_test

import (
	"math"
	"sort"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	commontypes "github.com/lavanet/lava/common/types"
	"github.com/lavanet/lava/testutil/common"
	testkeeper "github.com/lavanet/lava/testutil/keeper"
	"github.com/lavanet/lava/utils/slices"
	epochstoragetypes "github.com/lavanet/lava/x/epochstorage/types"
	pairingscores "github.com/lavanet/lava/x/pairing/keeper/scores"
	planstypes "github.com/lavanet/lava/x/plans/types"
	spectypes "github.com/lavanet/lava/x/spec/types"
	"github.com/stretchr/testify/require"
)

func TestPairingUniqueness(t *testing.T) {
	ts := newTester(t)
	ts.SetupAccounts(2, 0, 0) // 2 sub, 0 adm, 0 dev

	var balance int64 = 10000
	stake := balance / 10

	_, sub1Addr := ts.Account("sub1")
	_, sub2Addr := ts.Account("sub2")

	_, err := ts.TxSubscriptionBuy(sub1Addr, sub1Addr, ts.plan.Index, 1)
	require.Nil(t, err)
	_, err = ts.TxSubscriptionBuy(sub2Addr, sub2Addr, ts.plan.Index, 1)
	require.Nil(t, err)

	for i := 1; i <= 1000; i++ {
		_, addr := ts.AddAccount(common.PROVIDER, i, balance)
		err := ts.StakeProvider(addr, ts.spec, stake)
		require.Nil(t, err)
	}

	ts.AdvanceEpoch()

	// test that 2 different clients get different pairings
	pairing1, err := ts.QueryPairingGetPairing(ts.spec.Index, sub1Addr)
	require.Nil(t, err)
	pairing2, err := ts.QueryPairingGetPairing(ts.spec.Index, sub2Addr)
	require.Nil(t, err)

	filter := func(p epochstoragetypes.StakeEntry) string { return p.Address }

	providerAddrs1 := slices.Filter(pairing1.Providers, filter)
	providerAddrs2 := slices.Filter(pairing2.Providers, filter)

	require.Equal(t, len(pairing1.Providers), len(pairing2.Providers))
	require.False(t, slices.UnorderedEqual(providerAddrs1, providerAddrs2))

	ts.AdvanceEpoch()

	// test that in different epoch we get different pairings for consumer1
	pairing11, err := ts.QueryPairingGetPairing(ts.spec.Index, sub1Addr)
	require.Nil(t, err)

	providerAddrs11 := slices.Filter(pairing11.Providers, filter)

	require.Equal(t, len(pairing1.Providers), len(pairing11.Providers))
	require.False(t, slices.UnorderedEqual(providerAddrs1, providerAddrs11))

	// test that get pairing gives the same results for the whole epoch
	epochBlocks := ts.EpochBlocks()
	for i := uint64(0); i < epochBlocks-1; i++ {
		ts.AdvanceBlock()

		pairing111, err := ts.QueryPairingGetPairing(ts.spec.Index, sub1Addr)
		require.Nil(t, err)

		for i := range pairing11.Providers {
			providerAddr := pairing11.Providers[i].Address
			require.Equal(t, providerAddr, pairing111.Providers[i].Address)
			require.Nil(t, err)
			verify, err := ts.QueryPairingVerifyPairing(ts.spec.Index, sub1Addr, providerAddr, ts.BlockHeight())
			require.Nil(t, err)
			require.True(t, verify.Valid)
		}
	}
}

func TestValidatePairingDeterminism(t *testing.T) {
	ts := newTester(t)
	ts.SetupAccounts(1, 0, 0) // 1 sub, 0 adm, 0 dev

	var balance int64 = 10000
	stake := balance / 10

	_, sub1Addr := ts.Account("sub1")

	_, err := ts.TxSubscriptionBuy(sub1Addr, sub1Addr, ts.plan.Index, 1)
	require.Nil(t, err)

	for i := 1; i <= 10; i++ {
		_, addr := ts.AddAccount(common.PROVIDER, i, balance)
		err := ts.StakeProvider(addr, ts.spec, stake)
		require.Nil(t, err)
	}

	ts.AdvanceEpoch()

	// test that 2 different clients get different pairings
	pairing, err := ts.QueryPairingGetPairing(ts.spec.Index, sub1Addr)
	require.Nil(t, err)

	block := ts.BlockHeight()
	testAllProviders := func() {
		for _, provider := range pairing.Providers {
			providerAddr := provider.Address
			verify, err := ts.QueryPairingVerifyPairing(ts.spec.Index, sub1Addr, providerAddr, block)
			require.Nil(t, err)
			require.True(t, verify.Valid)
		}
	}

	count := ts.BlocksToSave() - ts.BlockHeight()
	for i := 0; i < int(count); i++ {
		ts.AdvanceBlock()
		testAllProviders()
	}
}

func TestGetPairing(t *testing.T) {
	ts := newTester(t)

	// do not use ts.setupForPayments(1, 1, 0), because it kicks off with AdvanceEpoch()
	// (for the benefit of users) but the "zeroEpoch" test below expects to start at the
	// same epoch of staking the providers.
	ts.addClient(1)
	ts.addProvider(1)

	// BLOCK_TIME = 30sec (testutil/keeper/keepers_init.go)
	constBlockTime := testkeeper.BLOCK_TIME
	epochBlocks := ts.EpochBlocks()

	// test: different epoch, valid tells if the payment request should work
	tests := []struct {
		name                string
		validPairingExists  bool
		isEpochTimesChanged bool
	}{
		{"zeroEpoch", false, false},
		{"firstEpoch", true, false},
		{"commonEpoch", true, false},
		{"epochTimesChanged", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Advance an epoch according to the test
			switch tt.name {
			case "zeroEpoch":
				// do nothing
			case "firstEpoch":
				ts.AdvanceEpoch()
			case "commonEpoch":
				ts.AdvanceEpochs(5)
			case "epochTimesChanged":
				ts.AdvanceEpochs(5)
				ts.AdvanceBlocks(epochBlocks/2, constBlockTime/2)
				ts.AdvanceBlocks(epochBlocks / 2)
			}

			_, clientAddr := ts.GetAccount(common.CONSUMER, 0)
			_, providerAddr := ts.GetAccount(common.PROVIDER, 0)

			// get pairing for client (for epoch zero expect to fail)
			pairing, err := ts.QueryPairingGetPairing(ts.spec.Index, clientAddr)
			if !tt.validPairingExists {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)

				// verify the expected provider
				require.Equal(t, providerAddr, pairing.Providers[0].Address)

				// verify the current epoch
				epochThis := ts.EpochStart()
				require.Equal(t, epochThis, pairing.CurrentEpoch)

				// verify the SpecLastUpdatedBlock
				require.Equal(t, ts.spec.BlockLastUpdated, pairing.SpecLastUpdatedBlock)

				// if prevEpoch == 0 -> averageBlockTime = 0
				// else calculate the time (like the actual get-pairing function)
				epochPrev, err := ts.Keepers.Epochstorage.GetPreviousEpochStartForBlock(ts.Ctx, epochThis)
				require.Nil(t, err)

				var averageBlockTime uint64
				if epochPrev != 0 {
					// calculate average block time base on total time from first block of
					// previous epoch until first block of this epoch and block dfference.
					blockCore1 := ts.Keepers.BlockStore.LoadBlock(int64(epochPrev))
					blockCore2 := ts.Keepers.BlockStore.LoadBlock(int64(epochThis))
					delta := blockCore2.Time.Sub(blockCore1.Time).Seconds()
					averageBlockTime = uint64(delta) / (epochThis - epochPrev)
				}

				overlapBlocks := ts.Keepers.Pairing.EpochBlocksOverlap(ts.Ctx)
				nextEpochStart, err := ts.Keepers.Epochstorage.GetNextEpoch(ts.Ctx, epochThis)
				require.Nil(t, err)

				// calculate the block in which the next pairing will happen (+overlap)
				nextPairingBlock := nextEpochStart + overlapBlocks
				// calculate number of blocks from the current block to the next epoch
				blocksUntilNewEpoch := nextPairingBlock - ts.BlockHeight()
				// calculate time left for the next pairing (blocks left* avg block time)
				timeLeftToNextPairing := blocksUntilNewEpoch * averageBlockTime

				if !tt.isEpochTimesChanged {
					require.Equal(t, timeLeftToNextPairing, pairing.TimeLeftToNextPairing)
				} else {
					// averageBlockTime in get-pairing query -> minimal average across sampled epoch
					// averageBlockTime in this test -> normal average across epoch
					// we've used a smaller blocktime some of the time -> averageBlockTime from
					// get-pairing is smaller than the averageBlockTime calculated in this test
					require.Less(t, pairing.TimeLeftToNextPairing, timeLeftToNextPairing)
				}

				// verify nextPairingBlock
				require.Equal(t, nextPairingBlock, pairing.BlockOfNextPairing)
			}
		})
	}
}

func TestPairingStatic(t *testing.T) {
	ts := newTester(t)
	ts.SetupAccounts(1, 0, 0) // 1 sub, 0 adm, 0 dev

	_, sub1Addr := ts.Account("sub1")

	ts.spec.ProvidersTypes = spectypes.Spec_static
	// will overwrite the default "mock" spec
	// (no TxProposalAddSpecs because the mock spec does not pass validaton)
	ts.AddSpec("mock", ts.spec)

	ts.AdvanceEpoch()

	_, err := ts.TxSubscriptionBuy(sub1Addr, sub1Addr, ts.plan.Index, 1)
	require.Nil(t, err)

	for i := 0; i < int(ts.plan.PlanPolicy.MaxProvidersToPair)*2; i++ {
		_, addr := ts.AddAccount(common.PROVIDER, i, testBalance)
		err := ts.StakeProvider(addr, ts.spec, testStake+int64(i))
		require.Nil(t, err)
	}

	// we expect to get all the providers in static spec

	ts.AdvanceEpoch()

	pairing, err := ts.QueryPairingGetPairing(ts.spec.Index, sub1Addr)
	require.Nil(t, err)

	for i, provider := range pairing.Providers {
		require.Equal(t, provider.Stake.Amount.Int64(), testStake+int64(i))
	}
}

func TestAddonPairing(t *testing.T) {
	ts := newTester(t)
	ts.setupForPayments(1, 0, 0) // 1 provider, 0 client, default providers-to-pair

	mandatory := spectypes.CollectionData{
		ApiInterface: "mandatory",
		InternalPath: "",
		Type:         "",
		AddOn:        "",
	}
	mandatoryAddon := spectypes.CollectionData{
		ApiInterface: "mandatory",
		InternalPath: "",
		Type:         "",
		AddOn:        "addon",
	}
	optional := spectypes.CollectionData{
		ApiInterface: "optional",
		InternalPath: "",
		Type:         "",
		AddOn:        "optional",
	}
	ts.spec.ApiCollections = []*spectypes.ApiCollection{
		{
			Enabled:        true,
			CollectionData: mandatory,
		},
		{
			Enabled:        true,
			CollectionData: optional,
		},
		{
			Enabled:        true,
			CollectionData: mandatoryAddon,
		},
	}

	// will overwrite the default "mock" spec
	ts.AddSpec("mock", ts.spec)
	specId := ts.spec.Index

	mandatoryChainPolicy := &planstypes.ChainPolicy{
		ChainId:     specId,
		Collections: []spectypes.CollectionData{mandatory},
	}
	mandatoryAddonChainPolicy := &planstypes.ChainPolicy{
		ChainId:     specId,
		Collections: []spectypes.CollectionData{mandatoryAddon},
	}
	optionalAddonChainPolicy := &planstypes.ChainPolicy{
		ChainId:     specId,
		Collections: []spectypes.CollectionData{optional},
	}
	optionalAndMandatoryAddonChainPolicy := &planstypes.ChainPolicy{
		ChainId:     specId,
		Collections: []spectypes.CollectionData{mandatoryAddon, optional},
	}

	templates := []struct {
		name                      string
		planChainPolicy           *planstypes.ChainPolicy
		subscChainPolicy          *planstypes.ChainPolicy
		projChainPolicy           *planstypes.ChainPolicy
		expectedProviders         int
		expectedStrictestPolicies []string
	}{
		{
			name:              "empty",
			expectedProviders: 12,
		},
		{
			name:              "mandatory in plan",
			planChainPolicy:   mandatoryChainPolicy,
			expectedProviders: 12, // stub provider also gets picked
		},
		{
			name:              "mandatory in subsc",
			subscChainPolicy:  mandatoryChainPolicy,
			projChainPolicy:   nil,
			expectedProviders: 12, // stub provider also gets picked
		},
		{
			name:              "mandatory in proj",
			projChainPolicy:   mandatoryChainPolicy,
			expectedProviders: 12, // stub provider also gets picked
		},
		{
			name:                      "addon in plan",
			planChainPolicy:           mandatoryAddonChainPolicy,
			subscChainPolicy:          nil,
			projChainPolicy:           nil,
			expectedProviders:         6,
			expectedStrictestPolicies: []string{"addon"},
		},
		{
			name:                      "addon in subsc",
			subscChainPolicy:          mandatoryAddonChainPolicy,
			expectedProviders:         6,
			expectedStrictestPolicies: []string{"addon"},
		},
		{
			name:                      "addon in proj",
			projChainPolicy:           mandatoryAddonChainPolicy,
			expectedProviders:         6,
			expectedStrictestPolicies: []string{"addon"},
		},
		{
			name:                      "optional in plan",
			planChainPolicy:           optionalAddonChainPolicy,
			expectedProviders:         7,
			expectedStrictestPolicies: []string{"optional"},
		},
		{
			name:                      "optional in subsc",
			subscChainPolicy:          optionalAddonChainPolicy,
			expectedProviders:         7,
			expectedStrictestPolicies: []string{"optional"},
		},
		{
			name:                      "optional in proj",
			projChainPolicy:           optionalAddonChainPolicy,
			expectedProviders:         7,
			expectedStrictestPolicies: []string{"optional"},
		},
		{
			name:                      "optional and addon in plan",
			planChainPolicy:           optionalAndMandatoryAddonChainPolicy,
			expectedProviders:         4,
			expectedStrictestPolicies: []string{"optional", "addon"},
		},
		{
			name:                      "optional and addon in subsc",
			subscChainPolicy:          optionalAndMandatoryAddonChainPolicy,
			expectedProviders:         4,
			expectedStrictestPolicies: []string{"optional", "addon"},
		},
		{
			name:                      "optional and addon in proj",
			projChainPolicy:           optionalAndMandatoryAddonChainPolicy,
			expectedProviders:         4,
			expectedStrictestPolicies: []string{"optional", "addon"},
		},
		{
			name:                      "optional and addon in plan, addon in subsc",
			planChainPolicy:           optionalAndMandatoryAddonChainPolicy,
			subscChainPolicy:          mandatoryAddonChainPolicy,
			expectedProviders:         4,
			expectedStrictestPolicies: []string{"optional", "addon"},
		},
		{
			name:                      "optional and addon in subsc, addon in plan",
			planChainPolicy:           mandatoryAddonChainPolicy,
			subscChainPolicy:          optionalAndMandatoryAddonChainPolicy,
			expectedProviders:         4,
			expectedStrictestPolicies: []string{"optional", "addon"},
		},
		{
			name:                      "optional and addon in subsc, addon in proj",
			subscChainPolicy:          optionalAndMandatoryAddonChainPolicy,
			projChainPolicy:           mandatoryAddonChainPolicy,
			expectedProviders:         4,
			expectedStrictestPolicies: []string{"optional", "addon"},
		},
		{
			name:                      "optional in subsc, addon in proj",
			subscChainPolicy:          optionalAndMandatoryAddonChainPolicy,
			projChainPolicy:           mandatoryAddonChainPolicy,
			expectedProviders:         4,
			expectedStrictestPolicies: []string{"optional", "addon"},
		},
	}

	mandatorySupportingEndpoints := []epochstoragetypes.Endpoint{{
		IPPORT:        "123",
		Geolocation:   1,
		Addons:        []string{mandatory.AddOn},
		ApiInterfaces: []string{mandatory.ApiInterface},
	}}
	mandatoryAddonSupportingEndpoints := []epochstoragetypes.Endpoint{{
		IPPORT:        "123",
		Geolocation:   1,
		Addons:        []string{mandatoryAddon.AddOn},
		ApiInterfaces: []string{mandatoryAddon.ApiInterface},
	}}
	mandatoryAndMandatoryAddonSupportingEndpoints := slices.Concat(
		mandatorySupportingEndpoints, mandatoryAddonSupportingEndpoints)

	optionalSupportingEndpoints := []epochstoragetypes.Endpoint{{
		IPPORT:        "123",
		Geolocation:   1,
		Addons:        []string{optional.AddOn},
		ApiInterfaces: []string{optional.ApiInterface},
	}}
	optionalAndMandatorySupportingEndpoints := slices.Concat(
		mandatorySupportingEndpoints, optionalSupportingEndpoints)
	optionalAndMandatoryAddonSupportingEndpoints := slices.Concat(
		mandatoryAddonSupportingEndpoints, optionalSupportingEndpoints)

	allSupportingEndpoints := slices.Concat(
		mandatorySupportingEndpoints, optionalAndMandatoryAddonSupportingEndpoints)

	mandatoryAndOptionalSingleEndpoint := []epochstoragetypes.Endpoint{{
		IPPORT:        "123",
		Geolocation:   1,
		Addons:        []string{},
		ApiInterfaces: []string{mandatoryAddon.ApiInterface, optional.ApiInterface},
	}}

	err := ts.addProviderEndpoints(2, mandatorySupportingEndpoints)
	require.NoError(t, err)
	err = ts.addProviderEndpoints(2, mandatoryAndMandatoryAddonSupportingEndpoints)
	require.NoError(t, err)
	err = ts.addProviderEndpoints(2, optionalAndMandatorySupportingEndpoints)
	require.NoError(t, err)
	err = ts.addProviderEndpoints(1, mandatoryAndOptionalSingleEndpoint)
	require.NoError(t, err)
	err = ts.addProviderEndpoints(2, optionalAndMandatoryAddonSupportingEndpoints)
	require.NoError(t, err)
	err = ts.addProviderEndpoints(2, allSupportingEndpoints)
	require.NoError(t, err)
	require.NoError(t, err)
	// total 11 providers

	err = ts.addProviderEndpoints(2, optionalSupportingEndpoints)
	require.Error(t, err)

	for _, tt := range templates {
		t.Run(tt.name, func(t *testing.T) {
			defaultPolicy := func() planstypes.Policy {
				return planstypes.Policy{
					ChainPolicies:      []planstypes.ChainPolicy{},
					GeolocationProfile: math.MaxUint64,
					MaxProvidersToPair: 12,
					TotalCuLimit:       math.MaxUint64,
					EpochCuLimit:       math.MaxUint64,
				}
			}

			plan := ts.plan // original mock template
			plan.PlanPolicy = defaultPolicy()

			if tt.planChainPolicy != nil {
				plan.PlanPolicy.ChainPolicies = []planstypes.ChainPolicy{*tt.planChainPolicy}
			}

			err := ts.TxProposalAddPlans(plan)
			require.Nil(t, err)

			_, sub1Addr := ts.AddAccount("sub", 0, 10000)

			_, err = ts.TxSubscriptionBuy(sub1Addr, sub1Addr, plan.Index, 1)
			require.Nil(t, err)

			// get the admin project and set its policies
			subProjects, err := ts.QuerySubscriptionListProjects(sub1Addr)
			require.Nil(t, err)
			require.Equal(t, 1, len(subProjects.Projects))

			projectID := subProjects.Projects[0]

			if tt.projChainPolicy != nil {
				projPolicy := defaultPolicy()
				projPolicy.ChainPolicies = []planstypes.ChainPolicy{*tt.projChainPolicy}
				_, err = ts.TxProjectSetPolicy(projectID, sub1Addr, projPolicy)
				require.Nil(t, err)
			}

			// apply policy change
			ts.AdvanceEpoch()

			if tt.subscChainPolicy != nil {
				subscPolicy := defaultPolicy()
				subscPolicy.ChainPolicies = []planstypes.ChainPolicy{*tt.subscChainPolicy}
				_, err = ts.TxProjectSetSubscriptionPolicy(projectID, sub1Addr, subscPolicy)
				require.Nil(t, err)
			}

			// apply policy change
			ts.AdvanceEpochs(2)

			project, err := ts.GetProjectForBlock(projectID, ts.BlockHeight())
			require.NoError(t, err)

			strictestPolicy, err := ts.Keepers.Pairing.GetProjectStrictestPolicy(ts.Ctx, project, specId)
			require.NoError(t, err)
			if len(tt.expectedStrictestPolicies) > 0 {
				require.NotEqual(t, 0, len(strictestPolicy.ChainPolicies))
				require.NotEqual(t, 0, len(strictestPolicy.ChainPolicies[0].Collections))
				addons := map[string]struct{}{}
				for _, collection := range strictestPolicy.ChainPolicies[0].Collections {
					if collection.AddOn != "" {
						addons[collection.AddOn] = struct{}{}
					}
				}
				for _, expected := range tt.expectedStrictestPolicies {
					_, ok := addons[expected]
					require.True(t, ok, "did not find addon in strictest policy %s, policy: %#v", expected, strictestPolicy)
				}
			}

			pairing, err := ts.QueryPairingGetPairing(ts.spec.Index, sub1Addr)
			if tt.expectedProviders > 0 {
				require.Nil(t, err)
				require.Equal(t, tt.expectedProviders, len(pairing.Providers), "received providers %#v", pairing)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestSelectedProvidersPairing(t *testing.T) {
	ts := newTester(t)

	ts.addProvider(200)

	policy := &planstypes.Policy{
		GeolocationProfile: math.MaxUint64,
		MaxProvidersToPair: 3,
	}

	allowed := planstypes.SELECTED_PROVIDERS_MODE_ALLOWED
	exclusive := planstypes.SELECTED_PROVIDERS_MODE_EXCLUSIVE
	disabled := planstypes.SELECTED_PROVIDERS_MODE_DISABLED

	maxProvidersToPair, err := ts.Keepers.Pairing.CalculateEffectiveProvidersToPairFromPolicies(
		[]*planstypes.Policy{&ts.plan.PlanPolicy, policy},
	)
	require.Nil(t, err)

	ts.addProvider(200)
	_, p1 := ts.GetAccount(common.PROVIDER, 0)
	_, p2 := ts.GetAccount(common.PROVIDER, 1)
	_, p3 := ts.GetAccount(common.PROVIDER, 2)
	_, p4 := ts.GetAccount(common.PROVIDER, 3)
	_, p5 := ts.GetAccount(common.PROVIDER, 4)

	providerSets := []struct {
		planProviders []string
		subProviders  []string
		projProviders []string
	}{
		{[]string{}, []string{}, []string{}},                                 // set #0
		{[]string{p1, p2, p3}, []string{}, []string{}},                       // set #1
		{[]string{p1, p2}, []string{}, []string{}},                           // set #2
		{[]string{p3, p4}, []string{}, []string{}},                           // set #3
		{[]string{p1, p2, p3}, []string{p1, p2}, []string{}},                 // set #4
		{[]string{p1, p2, p3}, []string{}, []string{p1, p3}},                 // set #5
		{[]string{}, []string{p1, p2, p3}, []string{p1, p2}},                 // set #6
		{[]string{p1}, []string{p1, p2, p3}, []string{p1, p2}},               // set #7
		{[]string{p1, p2, p3, p4, p5}, []string{p1, p2, p3, p4}, []string{}}, // set #8
	}

	expectedSelectedProviders := [][]string{
		{p1, p2, p3},     // expected providers for intersection of set #1
		{p1, p2},         // expected providers for intersection of set #2
		{p3, p4},         // expected providers for intersection of set #3
		{p1, p2},         // expected providers for intersection of set #4
		{p1, p3},         // expected providers for intersection of set #5
		{p1, p2},         // expected providers for intersection of set #6
		{p1},             // expected providers for intersection of set #7
		{p1, p2, p3, p4}, // expected providers for intersection of set #8
	}

	// TODO: add mixed mode test cases (once implemented)
	templates := []struct {
		name              string
		planMode          planstypes.SELECTED_PROVIDERS_MODE
		subMode           planstypes.SELECTED_PROVIDERS_MODE
		projMode          planstypes.SELECTED_PROVIDERS_MODE
		providersSet      int
		expectedProviders int
	}{
		// normal pairing cases
		{"ALLOWED mode normal pairing", allowed, allowed, allowed, 0, 0},
		{"DISABLED mode normal pairing", disabled, allowed, allowed, 0, 0},

		// basic pairing checks cases
		{"EXCLUSIVE mode selected MaxProvidersToPair providers", exclusive, allowed, allowed, 1, 0},
		{"EXCLUSIVE mode selected less than MaxProvidersToPair providers", exclusive, allowed, allowed, 2, 1},
		{"EXCLUSIVE mode selected less than MaxProvidersToPair different providers", exclusive, allowed, allowed, 3, 2},

		// intersection checks cases
		{"EXCLUSIVE mode intersection between plan/sub policies", exclusive, exclusive, exclusive, 4, 3},
		{"EXCLUSIVE mode intersection between plan/proj policies", exclusive, exclusive, exclusive, 5, 4},
		{"EXCLUSIVE mode intersection between sub/proj policies", exclusive, exclusive, exclusive, 6, 5},
		{"EXCLUSIVE mode intersection between all policies", exclusive, exclusive, exclusive, 7, 6},

		// selected providers more than MaxProvidersToPair
		{"EXCLUSIVE mode selected more than MaxProvidersToPair providers", exclusive, exclusive, exclusive, 8, 7},

		// provider unstake checks cases
		{"EXCLUSIVE mode provider unstakes after first pairing", exclusive, exclusive, exclusive, 1, 0},
		{"EXCLUSIVE mode non-staked provider stakes after first pairing", exclusive, exclusive, exclusive, 1, 0},
	}

	var expectedProvidersAfterUnstake []string

	for i, tt := range templates {
		t.Run(tt.name, func(t *testing.T) {
			_, sub1Addr := ts.AddAccount("sub", i, 10000)

			// create plan, propose it and buy subscription
			plan := ts.Plan("mock")
			providersSet := providerSets[tt.providersSet]

			plan.PlanPolicy.SelectedProvidersMode = tt.planMode
			plan.PlanPolicy.SelectedProviders = providersSet.planProviders

			err := ts.TxProposalAddPlans(plan)
			require.Nil(t, err)

			_, err = ts.TxSubscriptionBuy(sub1Addr, sub1Addr, plan.Index, 1)
			require.Nil(t, err)

			// get the admin project and set its policies
			res, err := ts.QuerySubscriptionListProjects(sub1Addr)
			require.Nil(t, err)
			require.Equal(t, 1, len(res.Projects))

			project, err := ts.GetProjectForBlock(res.Projects[0], ts.BlockHeight())
			require.Nil(t, err)

			policy.SelectedProvidersMode = tt.projMode
			policy.SelectedProviders = providersSet.projProviders

			_, err = ts.TxProjectSetPolicy(project.Index, sub1Addr, *policy)
			require.Nil(t, err)

			// skip epoch for the policy change to take effect
			ts.AdvanceEpoch()

			policy.SelectedProvidersMode = tt.subMode
			policy.SelectedProviders = providersSet.subProviders

			_, err = ts.TxProjectSetSubscriptionPolicy(project.Index, sub1Addr, *policy)
			require.Nil(t, err)

			// skip epoch for the policy change to take effect
			ts.AdvanceEpoch()
			// and another epoch to get pairing of two consecutive epochs
			ts.AdvanceEpoch()

			pairing, err := ts.QueryPairingGetPairing(ts.spec.Index, sub1Addr)
			require.Nil(t, err)

			providerAddresses1 := []string{}
			for _, provider := range pairing.Providers {
				providerAddresses1 = append(providerAddresses1, provider.Address)
			}

			if tt.name == "EXCLUSIVE mode provider unstakes after first pairing" {
				// unstake p1 and remove from expected providers
				_, err = ts.TxPairingUnstakeProvider(p1, ts.spec.Index)
				require.Nil(t, err)
				expectedProvidersAfterUnstake = expectedSelectedProviders[tt.expectedProviders][1:]
			} else if tt.name == "EXCLUSIVE mode non-staked provider stakes after first pairing" {
				err := ts.StakeProvider(p1, ts.spec, 10000000)
				require.Nil(t, err)
			}

			ts.AdvanceEpoch()

			pairing, err = ts.QueryPairingGetPairing(ts.spec.Index, sub1Addr)
			require.Nil(t, err)

			providerAddresses2 := []string{}
			for _, provider := range pairing.Providers {
				providerAddresses2 = append(providerAddresses2, provider.Address)
			}

			// check pairings
			switch tt.name {
			case "ALLOWED mode normal pairing", "DISABLED mode normal pairing":
				require.False(t, slices.UnorderedEqual(providerAddresses1, providerAddresses2))
				require.Equal(t, maxProvidersToPair, uint64(len(providerAddresses1)))
				require.Equal(t, maxProvidersToPair, uint64(len(providerAddresses2)))

			case "EXCLUSIVE mode selected MaxProvidersToPair providers":
				require.True(t, slices.UnorderedEqual(providerAddresses1, providerAddresses2))
				require.Equal(t, maxProvidersToPair, uint64(len(providerAddresses2)))
				require.True(t, slices.UnorderedEqual(expectedSelectedProviders[tt.expectedProviders], providerAddresses1))

			case "EXCLUSIVE mode selected less than MaxProvidersToPair providers",
				"EXCLUSIVE mode selected less than MaxProvidersToPair different providers",
				"EXCLUSIVE mode intersection between plan/sub policies",
				"EXCLUSIVE mode intersection between plan/proj policies",
				"EXCLUSIVE mode intersection between sub/proj policies",
				"EXCLUSIVE mode intersection between all policies":
				require.True(t, slices.UnorderedEqual(providerAddresses1, providerAddresses2))
				require.Less(t, uint64(len(providerAddresses1)), maxProvidersToPair)
				require.True(t, slices.UnorderedEqual(expectedSelectedProviders[tt.expectedProviders], providerAddresses1))

			case "EXCLUSIVE mode selected more than MaxProvidersToPair providers":
				require.True(t, slices.IsSubset(providerAddresses1, expectedSelectedProviders[tt.expectedProviders]))
				require.True(t, slices.IsSubset(providerAddresses2, expectedSelectedProviders[tt.expectedProviders]))
				require.Equal(t, maxProvidersToPair, uint64(len(providerAddresses1)))
				require.Equal(t, maxProvidersToPair, uint64(len(providerAddresses2)))

			case "EXCLUSIVE mode provider unstakes after first pairing":
				require.False(t, slices.UnorderedEqual(providerAddresses1, providerAddresses2))
				require.True(t, slices.UnorderedEqual(expectedSelectedProviders[tt.expectedProviders], providerAddresses1))
				require.True(t, slices.UnorderedEqual(expectedProvidersAfterUnstake, providerAddresses2))

			case "EXCLUSIVE mode non-staked provider stakes after first pairing":
				require.False(t, slices.UnorderedEqual(providerAddresses1, providerAddresses2))
				require.True(t, slices.UnorderedEqual(expectedSelectedProviders[tt.expectedProviders], providerAddresses2))
				require.True(t, slices.UnorderedEqual(expectedProvidersAfterUnstake, providerAddresses1))
			}
		})
	}
}

// Test that the pairing process picks identical providers uniformly
func TestPairingUniformDistribution(t *testing.T) {
	numIterations := 10000
	providersCount := 10
	providersToPair := 3

	ts := newTester(t)
	ts.setupForPayments(providersCount, 1, providersToPair)
	_, clientAddr := ts.GetAccount(common.CONSUMER, 0)

	// extend the subscription because we'll advance alot of epochs
	_, err := ts.TxSubscriptionBuy(clientAddr, clientAddr, ts.plan.Index, 10)
	require.Nil(t, err)

	// Create a map to count the occurrences of each provider
	providerCount := make(map[string]int)

	// Run the get-pairing function multiple times and count the occurrences of each provider
	for i := 0; i < numIterations; i++ {
		getPairingRes, err := ts.QueryPairingGetPairing(ts.spec.Index, clientAddr)
		require.Nil(t, err)

		pairedProviders := getPairingRes.Providers
		for _, provider := range pairedProviders {
			providerCount[provider.Address]++
		}

		ts.AdvanceEpoch() // advance epoch to change the pairing result
	}

	// Calculate the expected count for each provider (should be nearly equal for uniform distribution)
	expectedCount := (numIterations * providersToPair) / providersCount

	// Define a margin of error for the count (you can adjust this based on your tolerance)
	marginOfError := math.Round(0.1 * float64(expectedCount))

	// Check that the count for each provider is within the margin of error of the expected count
	for addr, count := range providerCount {
		if math.Abs(float64(count-expectedCount)) > marginOfError {
			t.Errorf("Provider with address %s was not picked with the expected weight: count = %d, expected = %d",
				addr, count, expectedCount)
		}
	}
}

// test to check that providers picks are aligned with their stake
// For example: providerA with stake=100 will be picked two times more than
// provider B with stake=50
func TestPairingDistributionPerStake(t *testing.T) {
	numIterations := 10000
	providersCount := 10
	providersToPair := 3

	ts := newTester(t)
	ts.setupForPayments(providersCount, 1, providersToPair)
	_, clientAddr := ts.GetAccount(common.CONSUMER, 0)

	// double the stake of one of the providers
	allProviders, err := ts.QueryPairingProviders(ts.spec.Index, false)
	require.Nil(t, err)
	doubleStakeProvider := allProviders.StakeEntry[0]
	doubleStake := doubleStakeProvider.Stake
	doubleStake.Amount = doubleStake.Amount.MulRaw(2)
	_, err = ts.TxPairingStakeProvider(
		doubleStakeProvider.Address,
		doubleStakeProvider.Chain,
		doubleStake,
		doubleStakeProvider.Endpoints,
		doubleStakeProvider.Geolocation,
		doubleStakeProvider.Moniker,
	)
	require.Nil(t, err)
	allProviders, err = ts.QueryPairingProviders(ts.spec.Index, false)
	require.Equal(t, providersCount, len(allProviders.StakeEntry))
	require.Nil(t, err)

	// extend the subscription because we'll advance alot of epochs
	_, err = ts.TxSubscriptionBuy(clientAddr, clientAddr, ts.plan.Index, 10)
	require.Nil(t, err)

	// Create a map to count the occurrences of each provider
	type providerInfo struct {
		count       int
		stakeAmount int64
	}
	providerCount := make(map[string]providerInfo)

	// Run the get-pairing function multiple times and count the occurrences of each provider
	for i := 0; i < numIterations; i++ {
		getPairingRes, err := ts.QueryPairingGetPairing(ts.spec.Index, clientAddr)
		require.Nil(t, err)

		for _, provider := range getPairingRes.Providers {
			if _, ok := providerCount[provider.Address]; !ok {
				info := providerInfo{count: 0, stakeAmount: provider.Stake.Amount.Int64()}
				providerCount[provider.Address] = info
			} else {
				info := providerCount[provider.Address]
				info.count++
				providerCount[provider.Address] = info
			}
		}

		ts.AdvanceEpoch() // advance epoch to change the pairing result
	}

	// Calculate the expected count for each provider based on their stakes
	var totalStakes int64
	for _, provider := range allProviders.StakeEntry {
		totalStakes += provider.Stake.Amount.Int64()
	}

	// Check that the count for each provider aligns with their stake's probability
	for addr, info := range providerCount {
		// Calculate the expected count based on the provider's stake
		expectedCount := providersToPair * (numIterations * int(info.stakeAmount)) / int(totalStakes)

		// Define a margin of error for the count (you can adjust this based on your tolerance)
		marginOfError := math.Round(0.15 * float64(expectedCount))

		if math.Abs(float64(info.count-expectedCount)) > marginOfError {
			t.Errorf("Provider with address %s was not picked with the expected weight: count = %d, expected = %d",
				addr, info.count, expectedCount)
		}
	}
}

func unorderedEqual(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	exists := make(map[string]bool)
	for _, value := range first {
		exists[value] = true
	}
	for _, value := range second {
		if !exists[value] {
			return false
		}
	}
	return true
}

func IsSubset(subset, superset []string) bool {
	// Create a map to store the elements of the superset
	elements := make(map[string]bool)

	// Populate the map with elements from the superset
	for _, elem := range superset {
		elements[elem] = true
	}

	// Check each element of the subset against the map
	for _, elem := range subset {
		if !elements[elem] {
			return false
		}
	}

	return true
}

func TestGeolocationPairingScores(t *testing.T) {
	ts := newTester(t)
	ts.setupForPayments(1, 3, 1)

	// for convinience
	GL := uint64(planstypes.Geolocation_value["GL"])
	USE := uint64(planstypes.Geolocation_value["USE"])
	EU := uint64(planstypes.Geolocation_value["EU"])
	AS := uint64(planstypes.Geolocation_value["AS"])
	AF := uint64(planstypes.Geolocation_value["AF"])
	AU := uint64(planstypes.Geolocation_value["AU"])
	USC := uint64(planstypes.Geolocation_value["USC"])
	USW := uint64(planstypes.Geolocation_value["USW"])
	USE_EU := USE + EU

	freePlanPolicy := planstypes.Policy{
		GeolocationProfile: 4, // USE
		TotalCuLimit:       10,
		EpochCuLimit:       2,
		MaxProvidersToPair: 5,
	}

	basicPlanPolicy := planstypes.Policy{
		GeolocationProfile: 0, // GLS
		TotalCuLimit:       10,
		EpochCuLimit:       2,
		MaxProvidersToPair: 14,
	}

	premiumPlanPolicy := planstypes.Policy{
		GeolocationProfile: 65535, // GL
		TotalCuLimit:       10,
		EpochCuLimit:       2,
		MaxProvidersToPair: 14,
	}

	// propose all plans and buy subscriptions
	freePlan := planstypes.Plan{
		Index:      "free",
		Block:      ts.BlockHeight(),
		Price:      sdk.NewCoin(epochstoragetypes.TokenDenom, sdk.NewInt(1)),
		PlanPolicy: freePlanPolicy,
	}

	basicPlan := planstypes.Plan{
		Index:      "basic",
		Block:      ts.BlockHeight(),
		Price:      sdk.NewCoin(epochstoragetypes.TokenDenom, sdk.NewInt(1)),
		PlanPolicy: basicPlanPolicy,
	}

	premiumPlan := planstypes.Plan{
		Index:      "premium",
		Block:      ts.BlockHeight(),
		Price:      sdk.NewCoin(epochstoragetypes.TokenDenom, sdk.NewInt(1)),
		PlanPolicy: premiumPlanPolicy,
	}

	plans := []planstypes.Plan{freePlan, basicPlan, premiumPlan}
	err := testkeeper.SimulatePlansAddProposal(ts.Ctx, ts.Keepers.Plans, plans)
	require.Nil(t, err)

	freeAcct, freeAddr := ts.GetAccount(common.CONSUMER, 0)
	basicAcct, basicAddr := ts.GetAccount(common.CONSUMER, 1)
	premiumAcct, premiumAddr := ts.GetAccount(common.CONSUMER, 2)

	ts.TxSubscriptionBuy(freeAddr, freeAddr, freePlan.Index, 1)
	ts.TxSubscriptionBuy(basicAddr, basicAddr, basicPlan.Index, 1)
	ts.TxSubscriptionBuy(premiumAddr, premiumAddr, premiumPlan.Index, 1)

	for geoName, geo := range planstypes.Geolocation_value {
		if geoName != "GL" && geoName != "GLS" {
			err = ts.addProviderGeolocation(5, uint64(geo))
			require.Nil(t, err)
		}
	}

	templates := []struct {
		name         string
		dev          common.Account
		planPolicy   planstypes.Policy
		changePolicy bool
		newGeo       uint64
		expectedGeo  []uint64
	}{
		// free plan (cannot change geolocation - verified in another test)
		{"default free plan", freeAcct, freePlanPolicy, false, 0, []uint64{USE}},

		// basic plan (cannot change geolocation - verified in another test)
		{"default basic plan", basicAcct, basicPlanPolicy, false, 0, []uint64{AF, AS, AU, EU, USE, USC, USW}},

		// premium plan (geolocation can change)
		{"default premium plan", premiumAcct, premiumPlanPolicy, false, 0, []uint64{AF, AS, AU, EU, USE, USC, USW}},
		{"premium plan - set policy regular geo", premiumAcct, premiumPlanPolicy, true, EU, []uint64{EU}},
		{"premium plan - set policy multiple geo", premiumAcct, premiumPlanPolicy, true, USE_EU, []uint64{EU, USE}},
		{"premium plan - set policy global geo", premiumAcct, premiumPlanPolicy, true, GL, []uint64{AF, AS, AU, EU, USE, USC, USW}},
	}

	for _, tt := range templates {
		t.Run(tt.name, func(t *testing.T) {
			devResponse, err := ts.QueryProjectDeveloper(tt.dev.Addr.String())
			require.Nil(t, err)

			projIndex := devResponse.Project.Index
			policies := []*planstypes.Policy{&tt.planPolicy}

			newPolicy := planstypes.Policy{}
			if tt.changePolicy {
				newPolicy = tt.planPolicy
				newPolicy.GeolocationProfile = tt.newGeo
				_, err = ts.TxProjectSetPolicy(projIndex, tt.dev.Addr.String(), newPolicy)
				require.Nil(t, err)
				policies = append(policies, &newPolicy)
			}

			ts.AdvanceEpoch() // apply the new policy

			providersRes, err := ts.QueryPairingProviders(ts.spec.Name, false)
			require.Nil(t, err)
			stakeEntries := providersRes.StakeEntry
			providerScores := []*pairingscores.PairingScore{}
			for i := range stakeEntries {
				providerScore := pairingscores.NewPairingScore(&stakeEntries[i])
				providerScores = append(providerScores, providerScore)
			}

			effectiveGeo, err := ts.Keepers.Pairing.CalculateEffectiveGeolocationFromPolicies(policies)
			require.Nil(t, err)

			slots := pairingscores.CalcSlots(planstypes.Policy{
				GeolocationProfile: effectiveGeo,
				MaxProvidersToPair: tt.planPolicy.MaxProvidersToPair,
			})

			geoSeen := map[uint64]bool{}
			for _, geo := range tt.expectedGeo {
				geoSeen[geo] = false
			}

			// calc scores and verify the scores are as expected
			for _, slot := range slots {
				err = pairingscores.CalcPairingScore(providerScores, pairingscores.GetStrategy(), slot)
				require.Nil(t, err)

				ok := verifyGeoScoreForTesting(providerScores, slot, geoSeen)
				require.True(t, ok)
			}

			// verify that the slots have all the expected geos
			for _, found := range geoSeen {
				require.True(t, found)
			}
		})
	}
}

func isGeoInList(geo uint64, geoList []uint64) bool {
	for _, geoElem := range geoList {
		if geoElem == geo {
			return true
		}
	}
	return false
}

// verifyGeoScoreForTesting is used to testing purposes only!
// it verifies that the max geo score are for providers that exactly match the geo req
// this function assumes that all the other reqs are equal (for example, stake req)
func verifyGeoScoreForTesting(providerScores []*pairingscores.PairingScore, slot *pairingscores.PairingSlot, expectedGeoSeen map[uint64]bool) bool {
	if slot == nil || len(providerScores) == 0 {
		return false
	}

	sort.Slice(providerScores, func(i, j int) bool {
		return providerScores[i].Score.GT(providerScores[j].Score)
	})

	geoReqObject := pairingscores.GeoReq{}
	geoReq, ok := slot.Reqs[geoReqObject.GetName()].(pairingscores.GeoReq)
	if !ok {
		return false
	}

	// verify that the geo is part of the expected geo
	_, ok = expectedGeoSeen[geoReq.Geo]
	if !ok {
		return false
	}
	expectedGeoSeen[geoReq.Geo] = true

	// verify that only providers that match with req geo have max score
	maxScore := providerScores[0].Score
	for _, score := range providerScores {
		if score.Provider.Geolocation == geoReq.Geo {
			if !score.Score.Equal(maxScore) {
				return false
			}
		} else {
			if score.Score.Equal(maxScore) {
				return false
			} else {
				break
			}
		}
	}

	return true
}

func TestDuplicateProviders(t *testing.T) {
	ts := newTester(t)
	ts.setupForPayments(1, 1, 1)

	basicPlanPolicy := planstypes.Policy{
		GeolocationProfile: 0, // GLS
		TotalCuLimit:       10,
		EpochCuLimit:       2,
		MaxProvidersToPair: 14,
	}

	basicPlan := planstypes.Plan{
		Index:      "basic",
		Block:      ts.BlockHeight(),
		Price:      sdk.NewCoin(epochstoragetypes.TokenDenom, sdk.NewInt(1)),
		PlanPolicy: basicPlanPolicy,
	}

	_, basicAddr := ts.GetAccount(common.CONSUMER, 0)

	err := testkeeper.SimulatePlansAddProposal(ts.Ctx, ts.Keepers.Plans, []planstypes.Plan{basicPlan})
	require.Nil(t, err)

	ts.AdvanceEpoch()
	ts.TxSubscriptionBuy(basicAddr, basicAddr, basicPlan.Index, 1)

	for geoName, geo := range planstypes.Geolocation_value {
		if geoName != "GL" && geoName != "GLS" {
			err := ts.addProviderGeolocation(5, uint64(geo))
			require.Nil(t, err)
		}
	}

	ts.AdvanceEpoch()

	for i := 0; i < 100; i++ {
		pairingRes, err := ts.QueryPairingGetPairing(ts.spec.Index, basicAddr)
		require.Nil(t, err)
		providerSeen := map[string]struct{}{}
		for _, provider := range pairingRes.Providers {
			_, found := providerSeen[provider.Address]
			require.False(t, found)
			providerSeen[provider.Address] = struct{}{}
		}
	}
}

// TestNoRequiredGeo checks that if no providers have the required geo, we still get a pairing list
func TestNoRequiredGeo(t *testing.T) {
	ts := newTester(t)
	ts.setupForPayments(1, 1, 5)

	freePlanPolicy := planstypes.Policy{
		GeolocationProfile: 4, // USE
		TotalCuLimit:       10,
		EpochCuLimit:       2,
		MaxProvidersToPair: 5,
	}

	freePlan := planstypes.Plan{
		Index:      "free",
		Block:      ts.BlockHeight(),
		Price:      sdk.NewCoin(epochstoragetypes.TokenDenom, sdk.NewInt(1)),
		PlanPolicy: freePlanPolicy,
	}

	_, freeAddr := ts.GetAccount(common.CONSUMER, 0)

	err := testkeeper.SimulatePlansAddProposal(ts.Ctx, ts.Keepers.Plans, []planstypes.Plan{freePlan})
	require.Nil(t, err)

	ts.AdvanceEpoch()
	ts.TxSubscriptionBuy(freeAddr, freeAddr, freePlan.Index, 1)

	// add 5 more providers that are not in US-E (the only allowed providers in the free plan)
	err = ts.addProviderGeolocation(5, uint64(planstypes.Geolocation_value["AS"]))
	require.Nil(t, err)

	ts.AdvanceEpoch()

	pairingRes, err := ts.QueryPairingGetPairing(ts.spec.Index, freeAddr)
	require.Nil(t, err)
	require.Equal(t, freePlanPolicy.MaxProvidersToPair, uint64(len(pairingRes.Providers)))
	for _, provider := range pairingRes.Providers {
		require.NotEqual(t, freePlanPolicy.GeolocationProfile, provider.Geolocation)
	}
}

// TestGeoSlotCalc checks that the calculated slots always hold a single bit geo req
func TestGeoSlotCalc(t *testing.T) {
	geoReqName := pairingscores.GeoReq{}.GetName()

	allGeos := planstypes.GetAllGeolocations()
	maxGeo := commontypes.FindMax(allGeos)

	// iterate over all possible geolocations, create a policy and calc slots
	// not checking 0 because there can never be a policy with geo=0
	for i := 1; i <= int(maxGeo); i++ {
		policy := planstypes.Policy{
			GeolocationProfile: uint64(i),
			MaxProvidersToPair: 14,
		}

		slots := pairingscores.CalcSlots(policy)
		for _, slot := range slots {
			geoReqFromMap := slot.Reqs[geoReqName]
			geoReq, ok := geoReqFromMap.(pairingscores.GeoReq)
			if !ok {
				require.Fail(t, "slot geo req is not of GeoReq type")
			}

			if !planstypes.IsGeoEnumSingleBit(int32(geoReq.Geo)) {
				require.Fail(t, "slot geo is not single bit")
			}
		}
	}

	// make sure the geo "GL" also works
	policy := planstypes.Policy{
		GeolocationProfile: uint64(planstypes.Geolocation_GL),
		MaxProvidersToPair: 14,
	}
	slots := pairingscores.CalcSlots(policy)
	for _, slot := range slots {
		geoReqFromMap := slot.Reqs[geoReqName]
		geoReq, ok := geoReqFromMap.(pairingscores.GeoReq)
		if !ok {
			require.Fail(t, "slot geo req is not of GeoReq type")
		}

		if !planstypes.IsGeoEnumSingleBit(int32(geoReq.Geo)) {
			require.Fail(t, "slot geo is not single bit")
		}
	}
}

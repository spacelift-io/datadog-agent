// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package sampler

import (
	"os"
	"testing"

	"github.com/DataDog/datadog-agent/pkg/config/remote"
	"github.com/DataDog/datadog-agent/pkg/trace/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const maxRemoteTPS = 12377

func TestRemoteConfInit(t *testing.T) {
	assert := assert.New(t)
	// disabled by default
	assert.Nil(newRemoteRates(0))
	// subscription to subscriber fails
	old := os.Getenv("DD_APM_FEATURES")
	os.Setenv("DD_APM_FEATURES", "remote_rates")
	assert.Nil(newRemoteRates(0))
	os.Setenv("DD_APM_FEATURES", old)
	// todo:raphael mock grpc server
}

func newTestRemoteRates() *RemoteRates {
	return &RemoteRates{
		maxSigTPS: maxRemoteTPS,
		samplers:  make(map[Signature]*remoteSampler),

		stopped: make(chan struct{}),
	}
}

func configGenerator(version uint64, rates pb.APMSampling) remote.APMSamplingUpdate {
	return remote.APMSamplingUpdate{
		Config: &remote.APMSamplingConfig{
			Config: remote.Config{
				ID:      "testid",
				Version: version,
			},
			Rates: []pb.APMSampling{rates},
		},
	}
}

func TestRemoteTPSUpdate(t *testing.T) {
	assert := assert.New(t)

	type sampler struct {
		service   string
		env       string
		targetTPS float64
		mechanism pb.SamplingMechanism
		rank      uint32
	}

	var testSteps = []struct {
		name             string
		ratesToApply     pb.APMSampling
		countServices    []ServiceSignature
		expectedSamplers []sampler
		version          uint64
	}{
		{
			name: "first rates received",
			ratesToApply: pb.APMSampling{
				TargetTPS: []pb.TargetTPS{
					{
						Service: "willBeRemoved",
						Value:   3.2,
					},
					{
						Service: "willBeRemoved",
						Env:     "env2",
						Value:   33,
					},
					{
						Service: "keep",
						Value:   1,
					},
				},
			},
			version: 30,
		},
		{
			name: "enable a sampler after counting a matching service",
			countServices: []ServiceSignature{
				{
					Name: "willBeRemoved",
				},
			},
			expectedSamplers: []sampler{
				{
					service:   "willBeRemoved",
					targetTPS: 3.2,
				},
			},
			version: 30,
		},
		{
			name: "nothing happens when counting a service not set remotely",
			countServices: []ServiceSignature{
				{
					Name: "no remote tps",
				},
			},
			expectedSamplers: []sampler{
				{
					service:   "willBeRemoved",
					targetTPS: 3.2,
				},
			},
			version: 30,
		},
		{
			name: "add 2 more samplers",
			countServices: []ServiceSignature{
				{
					Name: "keep",
				},
				{
					Name: "willBeRemoved",
					Env:  "env2",
				},
			},
			expectedSamplers: []sampler{
				{
					service:   "willBeRemoved",
					targetTPS: 3.2,
				},
				{
					service:   "willBeRemoved",
					env:       "env2",
					targetTPS: 33,
				},
				{
					service:   "keep",
					targetTPS: 1,
				},
			},
			version: 30,
		},
		{
			name: "receive new remote rates, non matching samplers are trimmed",
			ratesToApply: pb.APMSampling{
				TargetTPS: []pb.TargetTPS{
					{
						Service: "keep",
						Value:   27,
					},
				},
			},
			expectedSamplers: []sampler{
				{
					service:   "keep",
					targetTPS: 27,
				},
			},
			version: 35,
		},
		{
			name: "receive empty remote rates and above max",
			ratesToApply: pb.APMSampling{
				TargetTPS: []pb.TargetTPS{
					{
						Service: "keep",
						Value:   3718271,
					},
					{
						Service: "noop",
					},
				},
			},
			expectedSamplers: []sampler{
				{
					service:   "keep",
					targetTPS: maxRemoteTPS,
				},
			},
			version: 35,
		},
		{
			name: "keep highest rank",
			ratesToApply: pb.APMSampling{
				TargetTPS: []pb.TargetTPS{
					{
						Service:   "keep",
						Value:     10,
						Mechanism: 5,
						Rank:      3,
					},
					{
						Service:   "keep",
						Value:     10,
						Mechanism: 10,
						Rank:      10,
					},
					{
						Service:   "keep",
						Value:     10,
						Mechanism: 6,
						Rank:      6,
					},
				},
			},
			countServices: []ServiceSignature{{"keep", ""}},
			expectedSamplers: []sampler{
				{
					service:   "keep",
					targetTPS: 10,
					mechanism: 10,
					rank:      10,
				},
			},
		},
		{
			name: "duplicate",
			ratesToApply: pb.APMSampling{
				TargetTPS: []pb.TargetTPS{
					{
						Service: "keep",
						Value:   10,
						Rank:    3,
					},
					{
						Service: "keep",
						Value:   10,
						Rank:    3,
					},
				},
			},
			expectedSamplers: []sampler{
				{
					service:   "keep",
					targetTPS: 10,
					rank:      3,
				},
			},
		},
	}
	r := newTestRemoteRates()
	for _, step := range testSteps {
		t.Log(step.name)
		if step.ratesToApply.TargetTPS != nil {
			r.onUpdate(configGenerator(step.version, step.ratesToApply))
		}
		for _, s := range step.countServices {
			r.CountSignature(s.Hash())
		}

		assert.Len(r.samplers, len(step.expectedSamplers))

		for _, expectedS := range step.expectedSamplers {
			sig := ServiceSignature{Name: expectedS.service, Env: expectedS.env}.Hash()
			s, ok := r.samplers[sig]
			require.True(t, ok)
			root := &pb.Span{Metrics: map[string]float64{}}
			assert.Equal(expectedS.targetTPS, s.targetTPS.Load())
			assert.Equal(expectedS.mechanism, s.target.Mechanism)
			assert.Equal(expectedS.rank, s.target.Rank)
			r.CountSample(root, sig)

			tpsTag, ok := root.Metrics[tagRemoteTPS]
			assert.True(ok)
			assert.Equal(expectedS.targetTPS, tpsTag)
			versionTag, ok := root.Metrics[tagRemoteVersion]
			assert.True(ok)
			assert.Equal(float64(step.version), versionTag)
		}
	}
}

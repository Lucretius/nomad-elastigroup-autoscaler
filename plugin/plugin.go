package plugin

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-autoscaler/plugins"
	"github.com/hashicorp/nomad-autoscaler/plugins/base"
	"github.com/hashicorp/nomad-autoscaler/plugins/target"
	"github.com/hashicorp/nomad-autoscaler/sdk"
	"github.com/hashicorp/nomad-autoscaler/sdk/helper/nomad"
	"github.com/hashicorp/nomad-autoscaler/sdk/helper/scaleutils"
	"github.com/spotinst/spotinst-sdk-go/service/elastigroup"
	"github.com/spotinst/spotinst-sdk-go/spotinst/credentials"
	"github.com/spotinst/spotinst-sdk-go/spotinst/session"
)

const (
	pluginName = "spotinst-elastigroup"

	configKeyAccountID     = "account_id"
	configKeyToken         = "token"
	configKeyElastigroupID = "elastigroup_id"
	configKeyProvider      = "provider"
)

var (
	PluginConfig = &plugins.InternalPluginConfig{
		Factory: func(l hclog.Logger) interface{} { return NewSpotinstElastigroupPlugin(l) },
	}

	pluginInfo = &base.PluginInfo{
		Name:       pluginName,
		PluginType: sdk.PluginTypeTarget,
	}
)

// Assert that TargetPlugin meets the target.Target interface.
var _ target.Target = (*TargetPlugin)(nil)

// TargetPlugin is the DigitalOcean implementation of the target.Target interface.
type TargetPlugin struct {
	config   map[string]string
	logger   hclog.Logger

	client *elastigroup.ServiceOp

	// clusterUtils provides general cluster scaling utilities for querying the
	// state of nodes pools and performing scaling tasks.
	clusterUtils *scaleutils.ClusterScaleUtils
}

// NewSpotinstElastigroupPlugin returns the Spotinst Elastigroup implementation of the target.Target
// interface.
func NewSpotinstElastigroupPlugin(log hclog.Logger) *TargetPlugin {
	return &TargetPlugin{
		logger: log,
	}
}

// PluginInfo satisfies the PluginInfo function on the base.Base interface.
func (t *TargetPlugin) PluginInfo() (*base.PluginInfo, error) {
	return pluginInfo, nil
}

// SetConfig satisfies the SetConfig function on the base.Base interface.
func (t *TargetPlugin) SetConfig(config map[string]string) error {
	t.config = config

	sess := session.New()
	creds := credentials.NewChainCredentials(
		new(credentials.FileProvider),
		new(credentials.EnvProvider),
	)

	// try checking for credentials passed into the config
	if creds == nil {
		acctID, ok := t.config["account_id"]
		if !ok {
			return fmt.Errorf("account ID is required when using elastigroup")
		}
		token, ok := t.config["token"]
		if !ok {
			return fmt.Errorf("token is required when using elastigroup")
		}
		creds = credentials.NewStaticCredentials(token, acctID)
	}
	if creds == nil {
		return fmt.Errorf("unable to find Spotinst token")
	}
	t.client = elastigroup.New(sess)

	clusterUtils, err := scaleutils.NewClusterScaleUtils(nomad.ConfigFromNamespacedMap(config), t.logger)
	if err != nil {
		return err
	}

	// Store and set the remote ID callback function.
	t.clusterUtils = clusterUtils
	t.clusterUtils.ClusterNodeIDLookupFunc = spotinstElastigroupNodeIDMap

	return nil
}

// Scale satisfies the Scale function on the target.Target interface.
func (t *TargetPlugin) Scale(action sdk.ScalingAction, config map[string]string) error {
	// Spotinst can't support dry-run like Nomad, so just exit.
	if action.Count == sdk.StrategyActionMetaValueDryRunCount {
		return nil
	}

	ctx := context.Background()

	g, err := t.getCurrentElastigroup(ctx)
	if err != nil {
		return fmt.Errorf("failed to describe Elastigroup: %v", err)
	}
	direction := t.calculateDirection(g.Total, action.Count)

	if direction == "" {
		t.logger.Info("scaling not required", "current_count", g.Total, "strategy_count", action.Count)
		return nil
	}

	if direction != "out" && direction != "in" {
		t.logger.Error(fmt.Sprintf("cannot scale - scaling direction %s is invalid", direction))
	}

	if direction == "out" && g.MaxCapacity < action.Count {
		t.logger.Error("cannot scale out due to capacity limits", "current_count", g.Total, "strategy_count", action.Count)
	}

	if direction == "in" && g.MinCapacity > action.Count {
		t.logger.Error("cannot scale in due to capacity limits", "current_count", g.Total, "strategy_count", action.Count)
	}

	err = t.scale(ctx, action.Count, *g)
	// If we received an error while scaling, format this with an outer message
	// so its nice for the operators and then return any error to the caller.
	if err != nil {
		err = fmt.Errorf("failed to perform scaling action: %v", err)
	}
	return err
}

// Status satisfies the Status function on the target.Target interface.
func (t *TargetPlugin) Status(config map[string]string) (*sdk.TargetStatus, error) {
	// Perform our check of the Nomad node pool. If the pool is not ready, we
	// can exit here and avoid calling the Spotinst API as it won't affect the
	// outcome.
	ready, err := t.clusterUtils.IsPoolReady(config)
	if err != nil {
		return nil, fmt.Errorf("failed to run Nomad node readiness check: %v", err)
	}
	if !ready {
		return &sdk.TargetStatus{Ready: ready}, nil
	}

	ctx := context.Background()

	status, err := t.getCurrentElastigroupTargetStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to describe Elastigroup: %v", err)
	}

	resp := &sdk.TargetStatus{
		Ready: status.Ready,
		Count: status.Count,
		Meta:  make(map[string]string),
	}

	return resp, nil
}

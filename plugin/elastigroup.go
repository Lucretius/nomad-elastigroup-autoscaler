package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/nomad/api"
	"github.com/spotinst/spotinst-sdk-go/service/elastigroup/providers/aws"
	"github.com/spotinst/spotinst-sdk-go/service/elastigroup/providers/azure"
	"github.com/spotinst/spotinst-sdk-go/service/elastigroup/providers/gcp"
	"github.com/spotinst/spotinst-sdk-go/spotinst"
)

type SpotinstCloudProvider int

const (
	Azure SpotinstCloudProvider = iota
	AWS
	GCP
	Unknown
)

type Elastigroup struct {
	Group       interface{}
	Total       int64
	MaxCapacity int64
	MinCapacity int64
}

type ElastigroupStatus struct {
	Count int64
	Ready bool
}

// spotinstElastigroupNodeIDMap is used to identify the Spotinst VM of a Nomad node using
// the relevant attribute value.
func spotinstElastigroupNodeIDMap(n *api.Node) (string, error) {
	val, ok := n.Attributes["unique.hostname"]
	if !ok || val == "" {
		return "", fmt.Errorf("attribute %q not found", "unique.hostname")
	}
	return val, nil
}

func (t *TargetPlugin) getCurrentElastigroup(ctx context.Context) (*Elastigroup, error) {
	g := &Elastigroup{}
	provider, ok := t.config[configKeyProvider]; 
	if !ok {
		return nil, fmt.Errorf("provider is a required field")
	}
	groupID, ok := t.config[configKeyElastigroupID]; 
	if !ok {
		return nil, fmt.Errorf("elastigroup_id is a required field")
	}
	switch provider {
	case "azure":
		out, err := t.client.CloudProviderAzure().Read(ctx, &azure.ReadGroupInput{
			GroupID: spotinst.String(groupID),
		})
		if err != nil {
			return nil, fmt.Errorf("could not read Azure configuration: %w", err)
		}
		g.Group = out.Group
		g.Total = int64(*out.Group.Capacity.Target)
		g.MaxCapacity = int64(*out.Group.Capacity.Maximum)
		g.MinCapacity = int64(*out.Group.Capacity.Minimum)
	case "aws":
		out, err := t.client.CloudProviderAWS().Read(ctx, &aws.ReadGroupInput{
			GroupID: spotinst.String(groupID),
		})
		if err != nil {
			return nil, fmt.Errorf("could not read AWS configuration: %w", err)
		}
		g.Group = out.Group
		g.Total = int64(*out.Group.Capacity.Target)
		g.MaxCapacity = int64(*out.Group.Capacity.Maximum)
		g.MinCapacity = int64(*out.Group.Capacity.Minimum)
	case "gcp":
		out, err := t.client.CloudProviderGCP().Read(ctx, &gcp.ReadGroupInput{
			GroupID: spotinst.String(groupID),
		})
		if err != nil {
			return nil, fmt.Errorf("could not read GCP configuration: %w", err)
		}
		g.Group = out.Group
		g.Total = int64(*out.Group.Capacity.Target)
		g.MaxCapacity = int64(*out.Group.Capacity.Maximum)
		g.MinCapacity = int64(*out.Group.Capacity.Minimum)
	default:
		return nil, fmt.Errorf("expected provider of 'aws', 'azure', or 'gcp'.  Received %s ", provider)
	}
	return g, nil
}

// check the state of every VM in the elastigroup
// an elastigroup is considered "ready" for scaling when
// every VM in the elastigroup is in a running state
func (t *TargetPlugin) getCurrentElastigroupTargetStatus(ctx context.Context) (*ElastigroupStatus, error) {
	status := &ElastigroupStatus{}
	provider, ok := t.config[configKeyProvider]; 
	if !ok {
		return nil, fmt.Errorf("provider is a required field")
	}
	groupID, ok := t.config[configKeyElastigroupID]; 
	if !ok {
		return nil, fmt.Errorf("elastigroup_id is a required field")
	}
	switch provider {
	case "azure":
		out, err := t.client.CloudProviderAzure().Status(ctx, &azure.StatusGroupInput{
			GroupID: spotinst.String(groupID),
		})
		if err != nil {
			return nil, fmt.Errorf("could not read Azure elastigroup status: %w", err)
		}
		status.Count = int64(len(out.Nodes))
		status.Ready = true
		for _, vm := range out.Nodes {
			if !strings.EqualFold(spotinst.StringValue(vm.State), "running") {
				status.Ready = false
			}
		}
	case "aws":
		out, err := t.client.CloudProviderAWS().Status(ctx, &aws.StatusGroupInput{
			GroupID: spotinst.String(groupID),
		})
		if err != nil {
			return nil, fmt.Errorf("could not read AWS elastigroup status: %w", err)
		}
		status.Count = int64(len(out.Instances))
		status.Ready = true
		for _, vm := range out.Instances {
			if !strings.EqualFold(spotinst.StringValue(vm.Status), "running") {
				status.Ready = false
			}
		}
	case "gcp":
		out, err := t.client.CloudProviderGCP().Status(ctx, &gcp.StatusGroupInput{
			GroupID: spotinst.String(groupID),
		})
		if err != nil {
			return nil, fmt.Errorf("could not read GCP elastigroup status: %w", err)
		}
		status.Count = int64(len(out.Instances))
		status.Ready = true
		for _, vm := range out.Instances {
			if !strings.EqualFold(spotinst.StringValue(vm.StatusName), "running") {
				status.Ready = false
			}
		}
	default:
		return nil, fmt.Errorf("expected provider of 'aws', 'azure', or 'gcp'.  Received %s ", provider)
	}
	return status, nil
}

func (t *TargetPlugin) scale(ctx context.Context, count int64, g Elastigroup) error {
	var err error
	provider, ok := t.config[configKeyProvider]; 
	if !ok {
		return fmt.Errorf("provider is a required field")
	}
	
	switch provider {
	case "azure":
		azureGroup, ok := g.Group.(*azure.Group)
		azureGroup.Capacity.SetTarget(spotinst.Int(int(count)))
		if !ok {
			return fmt.Errorf("Group type assertion failed.  Group is not of type Azure")
		}
		_, err = t.client.CloudProviderAzure().Update(ctx, &azure.UpdateGroupInput{
			Group: azureGroup,
		})
	case "aws":
		awsGroup, ok := g.Group.(*aws.Group)
		if !ok {
			return fmt.Errorf("Group type assertion failed.  Group is not of type AWS")
		}
		awsGroup.Capacity.SetTarget(spotinst.Int(int(count)))
		_, err = t.client.CloudProviderAWS().Update(ctx, &aws.UpdateGroupInput{
			Group: awsGroup,
		})
	case "gcp":
		gcpGroup, ok := g.Group.(*gcp.Group)
		if !ok {
			return fmt.Errorf("Group type assertion failed.  Group is not of type GCP")
		}
		gcpGroup.Capacity.SetTarget(spotinst.Int(int(count)))
		_, err = t.client.CloudProviderGCP().Update(ctx, &gcp.UpdateGroupInput{
			Group: gcpGroup,
		})
	default:
		return fmt.Errorf("expected provider of 'aws', 'azure', or 'gcp'.  Received %s ", provider)
	}
	return err
}

func (t *TargetPlugin) calculateDirection(target, desired int64) string {
	if desired < target {
		return "in"
	}
	if desired > target {
		return "out"
	}
	return ""
}

func readElastigroupProvider(provider string) (SpotinstCloudProvider, error) {
	switch provider {
	case "Azure":
		return Azure, nil
	case "AWS":
		return AWS, nil
	case "GCP":
		return GCP, nil
	default:
		return Unknown, fmt.Errorf("Unknown provider: %s", provider)
	}
}

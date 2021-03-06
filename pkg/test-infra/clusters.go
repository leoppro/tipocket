package testinfra

import (
	"github.com/pingcap/tidb-operator/pkg/apis/pingcap/v1alpha1"
	"github.com/pingcap/tidb-operator/pkg/util/config"
	"golang.org/x/sync/errgroup"
	"k8s.io/utils/pointer"

	clusterTypes "github.com/pingcap/tipocket/pkg/cluster/types"
	"github.com/pingcap/tipocket/pkg/test-infra/binlog"
	"github.com/pingcap/tipocket/pkg/test-infra/cdc"
	"github.com/pingcap/tipocket/pkg/test-infra/fixture"
	"github.com/pingcap/tipocket/pkg/test-infra/tidb"
	"github.com/pingcap/tipocket/pkg/test-infra/tiflash"
	"github.com/pingcap/tipocket/pkg/test-infra/util"
)

// groupCluster creates clusters concurrently
type groupCluster struct {
	ops []clusterTypes.Cluster
}

// NewGroupCluster creates a groupCluster
func NewGroupCluster(clusters ...clusterTypes.Cluster) *groupCluster {
	return &groupCluster{ops: clusters}
}

// Apply creates the cluster
func (c *groupCluster) Apply() error {
	var g errgroup.Group
	num := len(c.ops)
	for i := 0; i < num; i++ {
		op := c.ops[i]
		g.Go(func() error {
			return op.Apply()
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	return nil
}

// Delete the cluster
func (c *groupCluster) Delete() error {
	var g errgroup.Group
	num := len(c.ops)
	for i := 0; i < num; i++ {
		op := c.ops[i]
		g.Go(func() error {
			return op.Delete()
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	return nil
}

// GetNodes returns the cluster nodes
func (c *groupCluster) GetNodes() ([]clusterTypes.Node, error) {
	var totalNodes []clusterTypes.Node
	for _, op := range c.ops {
		nodes, err := op.GetNodes()
		if err != nil {
			return nil, err
		}
		totalNodes = append(totalNodes, nodes...)
	}
	return totalNodes, nil
}

// GetClientNodes returns the client nodes
func (c *groupCluster) GetClientNodes() ([]clusterTypes.ClientNode, error) {
	var totalNodes []clusterTypes.ClientNode
	for _, op := range c.ops {
		nodes, err := op.GetClientNodes()
		if err != nil {
			return nil, err
		}
		totalNodes = append(totalNodes, nodes...)
	}
	return totalNodes, nil
}

// compositeCluster creates clusters sequentially
type compositeCluster struct {
	ops []clusterTypes.Cluster
}

// NewCompositeCluster creates a compositeCluster
func NewCompositeCluster(clusters ...clusterTypes.Cluster) *compositeCluster {
	return &compositeCluster{ops: clusters}
}

// Apply creates the cluster
func (c *compositeCluster) Apply() error {
	for _, op := range c.ops {
		if err := op.Apply(); err != nil {
			return err
		}
	}
	return nil
}

// Delete the cluster
func (c *compositeCluster) Delete() error {
	for _, op := range c.ops {
		if err := op.Delete(); err != nil {
			return err
		}
	}
	return nil
}

// GetNodes returns the cluster nodes
func (c *compositeCluster) GetNodes() ([]clusterTypes.Node, error) {
	var totalNodes []clusterTypes.Node
	for _, op := range c.ops {
		nodes, err := op.GetNodes()
		if err != nil {
			return nil, err
		}
		totalNodes = append(totalNodes, nodes...)
	}
	return totalNodes, nil
}

// GetClientNodes returns the client nodes
func (c *compositeCluster) GetClientNodes() ([]clusterTypes.ClientNode, error) {
	var totalNodes []clusterTypes.ClientNode
	for _, op := range c.ops {
		nodes, err := op.GetClientNodes()
		if err != nil {
			return nil, err
		}
		totalNodes = append(totalNodes, nodes...)
	}
	return totalNodes, nil
}

// NewDefaultCluster creates a new TiDB cluster
func NewDefaultCluster(namespace, name string, config fixture.TiDBClusterConfig) clusterTypes.Cluster {
	return tidb.New(namespace, name, config)
}

// NewCDCCluster creates two TiDB clusters with CDC
func NewCDCCluster(namespace, name string, conf fixture.TiDBClusterConfig) clusterTypes.Cluster {
	return NewCompositeCluster(
		NewGroupCluster(
			tidb.New(namespace, name+"-upstream", conf),
			tidb.New(namespace, name+"-downstream", conf),
		),
		cdc.New(namespace, name),
	)
}

// NewBinlogCluster creates two TiDB clusters with Binlog
func NewBinlogCluster(namespace, name string, conf fixture.TiDBClusterConfig) clusterTypes.Cluster {
	up := tidb.New(namespace, name+"-upstream", conf)
	upstream := up.GetTiDBCluster()
	upstream.Spec.TiDB.BinlogEnabled = pointer.BoolPtr(true)
	upstream.Spec.Pump = &v1alpha1.PumpSpec{
		Replicas:             3,
		ResourceRequirements: fixture.WithStorage(fixture.Small, "10Gi"),
		StorageClassName:     &fixture.Context.LocalVolumeStorageClass,
		ComponentSpec: v1alpha1.ComponentSpec{
			Image: util.BuildBinlogImage("tidb-binlog"),
		},
		GenericConfig: config.GenericConfig{
			Config: map[string]interface{}{},
		},
	}

	return NewCompositeCluster(
		NewGroupCluster(up, tidb.New(namespace, name+"-downstream", conf)),
		binlog.New(namespace, name),
	)
}

// NewABTestCluster creates two TiDB clusters to do AB Test
func NewABTestCluster(namespace, name string, confA, confB fixture.TiDBClusterConfig) clusterTypes.Cluster {
	return NewGroupCluster(
		tidb.New(namespace, name+"-a", confA),
		tidb.New(namespace, name+"-b", confB),
	)
}

// NewTiFlashCluster creates a TiDB cluster with TiFlash
func NewTiFlashCluster(namespace, name string, conf fixture.TiDBClusterConfig) clusterTypes.Cluster {
	t := tidb.New(namespace, name, conf)
	tc := t.GetTiDBCluster()
	// To make TiFlash work, we need to enable placement rules in pd.
	tc.Spec.PD.Config = &v1alpha1.PDConfig{
		Replication: &v1alpha1.PDReplicationConfig{
			EnablePlacementRules: pointer.BoolPtr(true),
		},
	}
	return NewCompositeCluster(t, tiflash.New(namespace, name))
}

// NewTiFlashABTestCluster creates two TiDB clusters to do AB Test, one with TiFlash
func NewTiFlashABTestCluster(namespace, name string, confA, confB fixture.TiDBClusterConfig) clusterTypes.Cluster {
	return NewGroupCluster(
		NewTiFlashCluster(namespace, name+"-a", confA),
		tidb.New(namespace, name+"-b", confB),
	)
}

// NewTiFlashCDCABTestCluster creates two TiDB clusters to do AB Test, one with TiFlash
// This also includes a CDC cluster between the two TiDB clusters.
func NewTiFlashCDCABTestCluster(namespace, name string, confA, confB fixture.TiDBClusterConfig) clusterTypes.Cluster {
	return NewCompositeCluster(
		NewGroupCluster(
			NewTiFlashCluster(namespace, name+"-upstream", confA),
			tidb.New(namespace, name+"-downstream", confB),
		),
		cdc.New(namespace, name),
	)
}

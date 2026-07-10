package store

import (
	"database/sql"
	"strings"
	"time"
)

func (s *Store) SaveOrchestrationCluster(cluster OrchestrationCluster) (OrchestrationCluster, error) {
	now := time.Now()
	if strings.TrimSpace(cluster.ID) == "" {
		cluster.ID = NewID("orchcluster")
	}
	if cluster.CreatedAt.IsZero() {
		cluster.CreatedAt = now
	}
	if strings.TrimSpace(cluster.Status) == "" {
		cluster.Status = "preparing"
	}
	if strings.TrimSpace(cluster.MetadataJSON) == "" {
		cluster.MetadataJSON = "{}"
	}
	cluster.UpdatedAt = now

	tx, err := s.db.Begin()
	if err != nil {
		return OrchestrationCluster{}, err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`insert into orchestration_clusters(
		id,action_id,package_name,package_version,name,topology,status,
		coordinator_server_id,task_id,credential_id,metadata_json,created_at,updated_at
	) values(?,?,?,?,?,?,?,?,?,?,?,?,?)
		on conflict(id) do update set
		action_id=excluded.action_id,package_name=excluded.package_name,package_version=excluded.package_version,
		name=excluded.name,topology=excluded.topology,status=excluded.status,
		coordinator_server_id=excluded.coordinator_server_id,task_id=excluded.task_id,
		credential_id=excluded.credential_id,metadata_json=excluded.metadata_json,updated_at=excluded.updated_at`,
		cluster.ID, cluster.ActionID, cluster.PackageName, cluster.PackageVersion, cluster.Name,
		cluster.Topology, cluster.Status, cluster.CoordinatorServerID, cluster.TaskID,
		cluster.CredentialID, cluster.MetadataJSON, cluster.CreatedAt, cluster.UpdatedAt)
	if err != nil {
		return OrchestrationCluster{}, err
	}
	if _, err := tx.Exec(`delete from orchestration_cluster_nodes where cluster_id=?`, cluster.ID); err != nil {
		return OrchestrationCluster{}, err
	}
	for index := range cluster.Nodes {
		node := &cluster.Nodes[index]
		if strings.TrimSpace(node.ID) == "" {
			node.ID = NewID("orchnode")
		}
		node.ClusterID = cluster.ID
		if node.Order <= 0 {
			node.Order = index + 1
		}
		if strings.TrimSpace(node.Status) == "" {
			node.Status = cluster.Status
		}
		if strings.TrimSpace(node.MetadataJSON) == "" {
			node.MetadataJSON = "{}"
		}
		if node.CreatedAt.IsZero() {
			node.CreatedAt = now
		}
		node.UpdatedAt = now
		_, err := tx.Exec(`insert into orchestration_cluster_nodes(
			id,cluster_id,server_id,role,endpoint,status,node_order,metadata_json,created_at,updated_at
		) values(?,?,?,?,?,?,?,?,?,?)`, node.ID, node.ClusterID, node.ServerID, node.Role,
			node.Endpoint, node.Status, node.Order, node.MetadataJSON, node.CreatedAt, node.UpdatedAt)
		if err != nil {
			return OrchestrationCluster{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return OrchestrationCluster{}, err
	}
	return cluster, nil
}

func (s *Store) ListOrchestrationClusters() ([]OrchestrationCluster, error) {
	rows, err := s.db.Query(`select id,action_id,package_name,package_version,name,topology,status,
		coordinator_server_id,task_id,coalesce(credential_id,''),coalesce(metadata_json,'{}'),created_at,updated_at
		from orchestration_clusters order by updated_at desc`)
	if err != nil {
		return nil, err
	}
	clusters := []OrchestrationCluster{}
	for rows.Next() {
		var cluster OrchestrationCluster
		if err := rows.Scan(&cluster.ID, &cluster.ActionID, &cluster.PackageName, &cluster.PackageVersion,
			&cluster.Name, &cluster.Topology, &cluster.Status, &cluster.CoordinatorServerID,
			&cluster.TaskID, &cluster.CredentialID, &cluster.MetadataJSON, &cluster.CreatedAt, &cluster.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		clusters = append(clusters, cluster)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range clusters {
		nodes, err := s.listOrchestrationClusterNodes(clusters[index].ID)
		if err != nil {
			return nil, err
		}
		clusters[index].Nodes = nodes
	}
	return clusters, nil
}

func (s *Store) GetOrchestrationCluster(id string) (OrchestrationCluster, error) {
	var cluster OrchestrationCluster
	err := s.db.QueryRow(`select id,action_id,package_name,package_version,name,topology,status,
		coordinator_server_id,task_id,coalesce(credential_id,''),coalesce(metadata_json,'{}'),created_at,updated_at
		from orchestration_clusters where id=?`, strings.TrimSpace(id)).Scan(
		&cluster.ID, &cluster.ActionID, &cluster.PackageName, &cluster.PackageVersion,
		&cluster.Name, &cluster.Topology, &cluster.Status, &cluster.CoordinatorServerID,
		&cluster.TaskID, &cluster.CredentialID, &cluster.MetadataJSON, &cluster.CreatedAt, &cluster.UpdatedAt)
	if err != nil {
		return OrchestrationCluster{}, err
	}
	cluster.Nodes, err = s.listOrchestrationClusterNodes(cluster.ID)
	return cluster, err
}

func (s *Store) FindOrchestrationCluster(actionID, name string) (OrchestrationCluster, error) {
	var cluster OrchestrationCluster
	err := s.db.QueryRow(`select id,action_id,package_name,package_version,name,topology,status,
		coordinator_server_id,task_id,coalesce(credential_id,''),coalesce(metadata_json,'{}'),created_at,updated_at
		from orchestration_clusters where action_id=? and name=? order by updated_at desc limit 1`,
		strings.TrimSpace(actionID), strings.TrimSpace(name)).Scan(
		&cluster.ID, &cluster.ActionID, &cluster.PackageName, &cluster.PackageVersion,
		&cluster.Name, &cluster.Topology, &cluster.Status, &cluster.CoordinatorServerID,
		&cluster.TaskID, &cluster.CredentialID, &cluster.MetadataJSON, &cluster.CreatedAt, &cluster.UpdatedAt)
	if err != nil {
		return OrchestrationCluster{}, err
	}
	cluster.Nodes, err = s.listOrchestrationClusterNodes(cluster.ID)
	return cluster, err
}

func (s *Store) listOrchestrationClusterNodes(clusterID string) ([]OrchestrationClusterNode, error) {
	rows, err := s.db.Query(`select id,cluster_id,server_id,role,coalesce(endpoint,''),status,node_order,
		coalesce(metadata_json,'{}'),created_at,updated_at
		from orchestration_cluster_nodes where cluster_id=? order by node_order,server_id`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []OrchestrationClusterNode{}
	for rows.Next() {
		var node OrchestrationClusterNode
		if err := rows.Scan(&node.ID, &node.ClusterID, &node.ServerID, &node.Role, &node.Endpoint,
			&node.Status, &node.Order, &node.MetadataJSON, &node.CreatedAt, &node.UpdatedAt); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (s *Store) DeleteOrchestrationCluster(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from orchestration_cluster_nodes where cluster_id=?`, strings.TrimSpace(id)); err != nil {
		return err
	}
	result, err := tx.Exec(`delete from orchestration_clusters where id=?`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

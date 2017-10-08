package server

// Alpha group is a group specialized for tracking network members and groups
// All nodes on the network should observe alpha group to provide group routing
// Alpha group will not track leadership changes, only members
// It also responsible for group creation and limit number of members in each group
// Both clients and cluster nodes can benefit form alpha group by bootstrapping with any node in the cluster
// It also provide valuable information for consistent hashing and distributed hash table implementations

// This file contains all of the functions for cluster nodes to track changes in alpha group


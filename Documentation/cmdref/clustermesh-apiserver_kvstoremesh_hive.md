<!-- This file was autogenerated via clustermesh-apiserver cmdref, do not edit manually-->

## clustermesh-apiserver kvstoremesh hive

Inspect the hive

```
clustermesh-apiserver kvstoremesh hive [flags]
```

### Options

```
      --api-serve-addr string                        Address to serve the KVStoreMesh API (default "localhost:9889")
      --cluster-id uint32                            Unique identifier of the cluster
      --cluster-name string                          Name of the cluster. It must consist of at most 32 lower case alphanumeric characters and '-', start and end with an alphanumeric character. (default "default")
      --clustermesh-config string                    Path to the ClusterMesh configuration directory
      --controller-group-metrics strings             List of controller group names for which to to enable metrics. Accepts 'all' and 'none'. The set of controller group names available is not guaranteed to be stable between Cilium versions.
  -D, --debug                                        Enable debugging mode
      --enable-gops                                  Enable gops server (default true)
      --enable-heartbeat                             KVStoreMesh will maintain heartbeat in destination etcd cluster
      --global-ready-timeout duration                KVStoreMesh will be considered ready even if any remote clusters have failed to synchronize within this duration (default 10m0s)
      --gops-port uint16                             Port for gops server to listen on (default 9894)
      --health-port int                              TCP port for ClusterMesh health API (default 9880)
  -h, --help                                         help for hive
      --kvstore string                               Key-value store type (default "etcd")
      --kvstore-lease-ttl duration                   Time-to-live for the KVstore lease. (default 15m0s)
      --kvstore-max-consecutive-quorum-errors uint   Max acceptable kvstore consecutive quorum errors before recreating the etcd connection (default 2)
      --kvstore-opt stringToString                   Key-value store options e.g. etcd.address=127.0.0.1:4001 (default [])
      --log-driver strings                           Logging endpoints to use (example: syslog)
      --log-opt map                                  Log driver options (example: format=json)
      --max-connected-clusters uint32                Maximum number of clusters to be connected in a clustermesh. Increasing this value will reduce the maximum number of identities available. Valid configurations are [255, 511]. (default 255)
      --per-cluster-ready-timeout duration           Remote clusters will be disregarded for readiness checks if a connection cannot be established within this duration (default 15s)
      --pprof                                        Enable serving pprof debugging API
      --pprof-address string                         Address that pprof listens on (default "localhost")
      --pprof-port uint16                            Port that pprof listens on (default 6064)
      --prometheus-serve-addr string                 Address to serve Prometheus metrics
```

### SEE ALSO

* [clustermesh-apiserver kvstoremesh](clustermesh-apiserver_kvstoremesh.md)	 - Run KVStoreMesh
* [clustermesh-apiserver kvstoremesh hive dot-graph](clustermesh-apiserver_kvstoremesh_hive_dot-graph.md)	 - Output the dependencies graph in graphviz dot format


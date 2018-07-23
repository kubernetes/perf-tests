set -o nounset
set -o pipefail

PERFTEST_ROOT=$(dirname "${BASH_SOURCE}")
while [ ! $# -eq 0 ]
do
       case "$1" in
               #CLUSTERLOADER
               --cluster-loader )
                       cd ${PERFTEST_ROOT}/clusterloader/e2e/ && go test -c -o e2e.test
                       ./e2e.test --ginkgo.v=true --ginkgo.focus="Cluster\sLoader" --kubeconfig="${HOME}/.kube/config" --viper-config=../config/test
                       exit
                       ;;
               --network-performance )
               #NETPERF
                       cd ${PERFTEST_ROOT}/network/benchmarks/netperf/ && go run ./launch.go  --kubeConfig="${HOME}/.kube/config" --hostnetworking --iterations 1
                       exit
                       ;;
               --kube-dns )
               #KUBE-DNS
                       cd ${PERFTEST_ROOT}/dns
                       mkdir out
                       ./run --params params/kubedns/default.yaml --out-dir out --use-cluster-dns
                       exit
                       ;;
               --core-dns )
               #CORE-DNS
                       cd ${PERFTEST_ROOT}/dns
                       ./run --dns-server coredns --params params/coredns/default.yaml --out-dir out
                       exit
                       ;;
               --help | -h )
                       echo  " --cluster-loader                Run Cluster Loader Test"
                       echo  " --network-performance           Run Network Performance Test"
                       echo  " --kube-dns                      Run Kube-DNS test"
                       echo  " --core-dns                      Run Core-DNS test"
                       exit
                       ;;

       esac
done

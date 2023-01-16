## **PostgreSQL benchmark on kubernetes**

---

### What is it?

This tool is written to check the performance of a PostgreSQL cluster on Kubernetes.

---

### Why do we need this?

When we are implementing an infrastructure, it is very important to predict the behavior of different system components in different environments and the differences between them.
This tool helps you check PostgreSQL performance in different environments.

---

### How does this work?

This tool first brings a postgresql cluster to a usable state with the help of kubegres. At the same time, a job tries to check the required items with the help of PGbench.

---

### What information does it give us?

*   The number of transactions that can be done per second in the database cluster.
*   The number of reads that can be done per second in the database cluster.
*   And most other things supported by pgbench.

---

### Requirements:

*   Kubernetes cluster in a healthy state and ready to work.
*   Kubegres operator in a healthy state and ready to work.
*   Jinja template tools

---

## Getting Started

*   First, if you have not installed kubgres, install it with the following command:  If you need more information, refer to [this link.](https://www.kubegres.io/doc/getting-started.html)

```plaintext
kubectl apply -f https://raw.githubusercontent.com/reactive-tech/kubegres/main/kubegres.yaml
```

*    In the second step, if you do not have jinja installed, install it with the following command:  If you need more information, refer to [this link.](https://pypi.org/project/jinja-cli/)

```plaintext
pip install jinja-cli
```

*   After installing the prerequisites, change the desired variables in the variables file (variables.yaml) For example, I change the following variables:

```plaintext
kubegres_namespace:
kubegres_storage_class_name:
kubegres_benchmark_transactions
kubegres_benchmark_clients:
```

> I strongly recommend that you take a look at the variables file and change them according to your needs.

*   At this step, place the variables in the manifest file with the following command:

```plaintext
jinja -d variables.yaml -o out.yaml postgresql.yaml
```

*   In the last step, execute the process with the following command:

```plaintext
kubectl apply -f out.yaml
```

---

### Where can I find the results?

Use the following command to see the results:

```plaintext
kubectl -n postgres-benchmark logs jobs/pgbench
```

An example of the result:

```plaintext

***Init***

dropping old tables...
NOTICE:  table "pgbench_accounts" does not exist, skipping
NOTICE:  table "pgbench_branches" does not exist, skipping
NOTICE:  table "pgbench_history" does not exist, skipping
NOTICE:  table "pgbench_tellers" does not exist, skipping
creating tables...
generating data (client-side)...
100000 of 100000 tuples (100%) done (elapsed 0.27 s, remaining 0.00 s)
vacuuming...
creating primary keys...
done in 2.60 s (drop tables 0.01 s, create tables 1.34 s, client-side generate 0.63 s, vacuum 0.20 s, primary keys 0.41 s).

***Master Transation benchmark***

pgbench (15.1 (Debian 15.1-1.pgdg110+1), server 14.1 (Debian 14.1-1.pgdg110+1))
starting vacuum...end.
transaction type: <builtin: TPC-B (sort of)>
scaling factor: 1
query mode: simple
number of clients: 1
number of threads: 1
maximum number of tries: 1
number of transactions per client: 10000
number of transactions actually processed: 10000/10000
number of failed transactions: 0 (0.000%)
latency average = 15.053 ms
initial connection time = 6.864 ms
tps = 66.431146 (without initial connection time)

***Replica readonly benchmark***

pgbench (15.1 (Debian 15.1-1.pgdg110+1), server 14.1 (Debian 14.1-1.pgdg110+1))
transaction type: <builtin: select only>
scaling factor: 1
query mode: simple
number of clients: 1
number of threads: 1
maximum number of tries: 1
number of transactions per client: 10000
number of transactions actually processed: 10000/10000
number of failed transactions: 0 (0.000%)
latency average = 0.444 ms
initial connection time = 18.918 ms
tps = 2252.012849 (without initial connection time)
```
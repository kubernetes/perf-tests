#!/usr/bin/env python3

import argparse
import concurrent.futures
import threading
import time
from kubernetes import client, config

BATCH_SIZE = 1000
PARALLEL = 20
PREFIX = "direct-scheduler-throughput-pod"
STORAGE_CLASS = "sched-test"

config.load_kube_config()

_local = threading.local()


def get_core_api():
    if not hasattr(_local, "core_v1"):
        _local.core_v1 = client.CoreV1Api()
    return _local.core_v1


def create_pv(i, namespace):
    api = get_core_api()
    pv = client.V1PersistentVolume(
        metadata=client.V1ObjectMeta(name=f"{PREFIX}-pv-{i}"),
        spec=client.V1PersistentVolumeSpec(
            capacity={"storage": "1Gi"},
            access_modes=["ReadWriteOnce"],
            storage_class_name=STORAGE_CLASS,
            volume_mode="Filesystem",
            persistent_volume_reclaim_policy="Delete",
            claim_ref=client.V1ObjectReference(
                namespace=namespace,
                name=f"{PREFIX}-pvc-{i}",
            ),
            host_path=client.V1HostPathVolumeSource(
                path="/tmp/sched-test",
                type="DirectoryOrCreate",
            ),
        ),
    )
    try:
        api.create_persistent_volume(pv)
    except client.exceptions.ApiException as e:
        if e.status == 409:
            pass
        else:
            raise


def run_batch(start, end, namespace):
    for i in range(start, end + 1):
        create_pv(i, namespace)
    print(f"Batch {start}-{end} done ({end - start + 1} PVs)")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--total", type=int, default=100_000)
    parser.add_argument("--start", type=int, default=1, help="Resume from this index (inclusive)")
    parser.add_argument("--parallel", type=int, default=PARALLEL)
    parser.add_argument("--batch-size", type=int, default=BATCH_SIZE)
    parser.add_argument("--namespace", type=str, default="default", help="Namespace for claimRef")
    args = parser.parse_args()

    start_idx = args.start
    end_idx = args.total
    batch_size = args.batch_size
    total_to_create = end_idx - start_idx + 1

    batches = []
    for s in range(start_idx, end_idx + 1, batch_size):
        e = min(s + batch_size - 1, end_idx)
        batches.append((s, e))

    print(f"Creating PVs {start_idx}-{end_idx} ({total_to_create} PVs) with {args.parallel} workers...")
    start_time = time.time()

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.parallel) as pool:
        futures = [pool.submit(run_batch, s, e, args.namespace) for s, e in batches]
        concurrent.futures.wait(futures)
        for f in futures:
            f.result()

    elapsed = time.time() - start_time
    print(f"Done in {elapsed:.1f}s ({total_to_create / elapsed:.0f} PVs/s)")


if __name__ == "__main__":
    main()

SELECT
    runs.run_subid as subid,
    runs.query_file as source_file,
    ROUND(AVG(results.min_latency), 4) as query_min_latency,
    ROUND(AVG(results.avg_latency), 4) as query_avg_latency,
    ROUND(AVG(results.max_latency), 4) as query_max_latency,
    ROUND(AVG(results.stddev_latency), 4) as query_stddev,
    IFNULL(MAX(runs.max_qps), "NO CAP") as dnsperf_max_qps,
    ROUND(MAX(results.qps), 2) as max_achieved_qps
FROM runs
LEFT JOIN
    results on
        results.run_id = runs.run_id AND
        results.run_subid = runs.run_subid
GROUP BY (runs.run_subid)

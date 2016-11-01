SELECT
  run_id, run_subid, dnsmasq_cpu, kubedns_cpu, max_qps, query_file,
  '--',
  qps, latency_95_percentile
FROM
  results NATURAL JOIN runs
WHERE
  results.latency_95_percentile < 20 -- ms
  AND results.run_id = runs.run_id
  AND results.run_subid = runs.run_subid
ORDER BY
  qps ASC;

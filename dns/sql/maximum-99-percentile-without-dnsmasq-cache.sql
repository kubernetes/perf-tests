SELECT
  max(latency_99_percentile)
FROM
  results NATURAL JOIN runs -- equijoin on run_id, run_subid
WHERE
  dnsmasq_cache = 0;

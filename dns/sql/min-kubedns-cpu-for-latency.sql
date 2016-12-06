-- Get the minimum kubedns cpu setting that has 95th percentile < 50ms
-- while serving 2k QPS and query is served by kubedns only.
SELECT
	run_id,
	run_subid,
	min(kubedns_cpu),
	'---',
	qps,
	latency_95_percentile
FROM
	runs NATURAL JOIN results
WHERE
	latency_95_percentile <= 50
	AND dnsmasq_cache = 0
	AND max_qps >= 2000
	AND query_file = 'service.txt'
;

-- wrk script used to get the results in a computer-friendly format.
function done(summary, latency, requests)
	local errors = summary.errors.connect + summary.errors.read +
		summary.errors.write + summary.errors.status +
		summary.errors.timeout

	f = assert(io.open("wrkout.csv", "w+"))

	f:write("duration,requests,bytes,errors,")
	f:write("reqps,byteps,latmean,")
	f:write("lat50,lat90,lat99,lat99.9,lat99.99,lat99.999\n")

	f:write(string.format("%d,%d,%d,%d,",
		summary.duration, summary.requests, summary.bytes, errors))
	f:write(string.format("%f,%f,%f,",
		summary.requests / (summary.duration/1000000.0),
		summary.bytes / (summary.duration/1000000.0),
		latency.mean))
	for _, p in pairs({ 50, 90, 99, 99.9, 99.99, 99.999 }) do
		n = latency:percentile(p)
		f:write(string.format("%d,", n))
	end
	f:write("\n")
end

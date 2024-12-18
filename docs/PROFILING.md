# Profiling

- build load test using `make build-loadtest`
- start the stack using `make profile-docker`
- start profiling memory in one terminal using `go tool pprof -http=:8081 http://localhost:9090/debug/pprof/heap\?seconds\=600` (URL should point to server:6060 endpoint)
- start profiling CPU in another terminal using `go tool pprof -http=:8082 http://localhost:9090/debug/pprof/profile\?seconds\=600`
- seed **once** with `bin/loadtest -mode seed -env ./docker/pc.env.loadtest`
- start load test using `bin/loadtest -mode test -env ./docker/pc.env.loadtest -duration 600 -rps 450 -sitekey-percent 70` (obviously you can play with args)

After the profiling is finished, browser links will open with flamegraph view option.

Open [Local Grafana](http://localhost:3000) and explore dashboards (credentials are admin:admin).

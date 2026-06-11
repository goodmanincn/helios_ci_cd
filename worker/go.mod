module github.com/helios-cicd/helios/worker

go 1.25.0

require (
	github.com/helios-cicd/helios/api v0.0.0
	github.com/hibiken/asynq v0.26.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/docker/docker v28.5.2+incompatible // indirect
	github.com/docker/go-connections v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/redis/go-redis/v9 v9.20.0 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	k8s.io/api v0.32.0 // indirect
)

replace github.com/helios-cicd/helios/api => ../api

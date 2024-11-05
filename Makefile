gen:
	@echo "Generating Proto files"
	@protoc --go_out=internal/client/tracer/ --go_opt=paths=source_relative --go-grpc_out=internal/client/tracer/ --go-grpc_opt=paths=source_relative --proto_path=internal/client/tracer tracer.proto

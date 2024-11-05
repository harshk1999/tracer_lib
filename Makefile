gen:
	@echo "Generating Proto files"
	@protoc --go_out=client/tracer/ --go_opt=paths=source_relative --go-grpc_out=client/tracer/ --go-grpc_opt=paths=source_relative --proto_path=client/tracer tracer.proto

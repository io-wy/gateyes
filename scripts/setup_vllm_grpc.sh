#!/bin/bash
set -e
cd /home/io/code/vLLM_SGLang_cuteDSL_tutorial/vllm
source .venv/bin/activate

# Kill existing vllm serve processes
pkill -f "vllm serve" || true
sleep 2

# Install gRPC dependency
pip install vllm[grpc]

# Start gRPC server
nohup python -m vllm.entrypoints.grpc_server \
  --model Qwen/Qwen2.5-0.5B-Instruct \
  --gpu-memory-utilization 0.7 \
  --max-model-len 4096 \
  --host 0.0.0.0 \
  > /tmp/vllm_grpc.log 2>&1 &

echo "vLLM gRPC server started, PID: $!"
sleep 10
ss -tlnp | grep 50051 && echo "PORT 50051 LISTENING" || echo "PORT 50051 NOT LISTENING"

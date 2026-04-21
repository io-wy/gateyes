package provider

import (
	"context"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/dynamicpb"
)

func (p *grpcProvider) StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error) {
	result := make(chan ResponseEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(result)
		defer close(errCh)

		if err := p.validateRequest(req); err != nil {
			errCh <- err
			return
		}

		callCtx, cancel := providerCallContext(ctx, p.cfg.Timeout)
		defer cancel()

		descriptors, err := loadVLLMGRPCDescriptors()
		if err != nil {
			errCh <- newProviderConfigError("provider.grpc.descriptors", err.Error())
			return
		}
		conn, outgoing, err := p.prepareCall(callCtx)
		if err != nil {
			errCh <- err
			return
		}
		decoder, err := p.getDecoder(callCtx, conn, outgoing)
		if err != nil {
			errCh <- err
			return
		}
		request, err := buildVLLMGRPCGenerateRequest(descriptors, req)
		if err != nil {
			errCh <- newProviderConfigError("provider.grpc.build_request", err.Error())
			return
		}
		stream, err := openVLLMGRPCGenerateStream(callCtx, conn, outgoing, request)
		if err != nil {
			errCh <- err
			return
		}

		var outputIDs []uint32
		var lastText string
		var finalUsage Usage
		var finalReason string

		for {
			message := dynamicpb.NewMessage(descriptors.generateResponse)
			if err := stream.RecvMsg(message); err != nil {
				if err == io.EOF {
					resp := NewTextResponse("", req.Model, lastText, finalUsage)
					if finalReason == "abort" {
						resp.Status = "cancelled"
					}
					result <- ResponseEvent{Type: EventResponseCompleted, Response: resp}
					return
				}
				errCh <- grpcError(err)
				return
			}

			chunk, complete := parseVLLMGRPCGenerateResponse(descriptors, message)
			if chunk != nil {
				outputIDs = append(outputIDs, chunk.TokenIDs...)
				finalUsage = chunk.Usage
				delta, current, err := decodeVLLMTextDelta(decoder, outputIDs, lastText)
				if err != nil {
					errCh <- newProviderParseError("provider.grpc.decode_stream", err, "decode vllm grpc stream chunk")
					return
				}
				lastText = current
				if delta != "" {
					usage := finalUsage
					result <- ResponseEvent{
						Type:      EventContentDelta,
						Delta:     delta,
						TextDelta: delta,
						Usage:     &usage,
					}
				}
				continue
			}
			if complete == nil {
				continue
			}

			if len(complete.OutputIDs) > 0 {
				outputIDs = append(outputIDs[:0], complete.OutputIDs...)
				delta, current, err := decodeVLLMTextDelta(decoder, outputIDs, lastText)
				if err != nil {
					errCh <- newProviderParseError("provider.grpc.decode_complete", err, "decode vllm grpc complete output")
					return
				}
				lastText = current
				if delta != "" {
					usage := complete.Usage
					result <- ResponseEvent{
						Type:      EventContentDelta,
						Delta:     delta,
						TextDelta: delta,
						Usage:     &usage,
					}
				}
			}
			finalUsage = complete.Usage
			finalReason = complete.FinishReason
		}
	}()

	return result, errCh
}

func openVLLMGRPCGenerateStream(ctx context.Context, conn *grpc.ClientConn, outgoing metadata.MD, request *dynamicpb.Message) (grpc.ClientStream, error) {
	streamDesc := &grpc.StreamDesc{ServerStreams: true}
	callCtx := ctx
	if len(outgoing) > 0 {
		callCtx = metadata.NewOutgoingContext(ctx, outgoing)
	}
	stream, err := conn.NewStream(callCtx, streamDesc, "/vllm.grpc.engine.VllmEngine/Generate")
	if err != nil {
		return nil, grpcError(err)
	}
	if err := stream.SendMsg(request); err != nil {
		return nil, grpcError(err)
	}
	if err := stream.CloseSend(); err != nil {
		return nil, grpcError(err)
	}
	return stream, nil
}

func collectVLLMGenerateOutput(stream grpc.ClientStream, descriptors *vllmGRPCDescriptors) ([]uint32, Usage, string, error) {
	var outputIDs []uint32
	var usage Usage
	var finishReason string

	for {
		message := dynamicpb.NewMessage(descriptors.generateResponse)
		if err := stream.RecvMsg(message); err != nil {
			if err == io.EOF {
				return outputIDs, usage, finishReason, nil
			}
			return nil, Usage{}, "", grpcError(err)
		}
		chunk, complete := parseVLLMGRPCGenerateResponse(descriptors, message)
		if chunk != nil {
			outputIDs = append(outputIDs, chunk.TokenIDs...)
			usage = chunk.Usage
			continue
		}
		if complete != nil {
			if len(complete.OutputIDs) > 0 {
				outputIDs = append(outputIDs[:0], complete.OutputIDs...)
			}
			usage = complete.Usage
			finishReason = complete.FinishReason
		}
	}
}

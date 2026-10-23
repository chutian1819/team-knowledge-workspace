package langfuse

import (
	"context"
	"errors"
	"sort"

	"go.opentelemetry.io/otel/attribute"
)

type observationContextKey struct{}
type observationContext struct {
	metadata   map[string]interface{}
	identity   []attribute.KeyValue
	safeErrors bool
}

func observationContextFrom(ctx context.Context) observationContext {
	value, _ := ctx.Value(observationContextKey{}).(observationContext)
	return value
}

// WithObservationMetadata copies correlation labels to descendant spans and
// model observations. Callers must pass identifiers/versions, never credentials.
// Copy-on-write keeps sibling operations from mutating one another's labels.
func WithObservationMetadata(ctx context.Context, metadata map[string]interface{}) context.Context {
	value := observationContextFrom(ctx)
	value.metadata = mergeMetadata(value.metadata, metadata)
	return context.WithValue(ctx, observationContextKey{}, value)
}

// ObservationMetadataFromContext returns a copy for orchestration and tests.
func ObservationMetadataFromContext(ctx context.Context) map[string]interface{} {
	return mergeMetadata(nil, observationContextFrom(ctx).metadata)
}

func withObservationIdentity(ctx context.Context, identity []attribute.KeyValue) context.Context {
	value := observationContextFrom(ctx)
	value.identity = append(append([]attribute.KeyValue{}, value.identity...), identity...)
	return context.WithValue(ctx, observationContextKey{}, value)
}

// WithObservationIdentity preserves user/session labels across async boundaries
// and attaches them to every observation (required by Langfuse v4 filtering).
func WithObservationIdentity(ctx context.Context, userID, sessionID string) context.Context {
	attrs := []attribute.KeyValue{}
	if userID != "" {
		attrs = append(attrs, attribute.String(attrUserID, userID))
	}
	if sessionID != "" {
		attrs = append(attrs, attribute.String(attrSessionID, sessionID))
	}
	return withObservationIdentity(ctx, attrs)
}

// WithSafeObservationErrors prevents credential-bearing provider error bodies
// from being copied to tracing. The original application error is unchanged.
func WithSafeObservationErrors(ctx context.Context) context.Context {
	value := observationContextFrom(ctx)
	value.safeErrors = true
	return context.WithValue(ctx, observationContextKey{}, value)
}

func observationError(err error, safe bool) error {
	if err == nil || !safe {
		return err
	}
	return errors.New("research operation failed; inspect the application result")
}

// Keep the packed attribute for existing readers, and emit explicit metadata
// attributes for the v4 observations API's filterable metadata mapping.
func metadataAttrs(prefix string, metadata map[string]interface{}) []attribute.KeyValue {
	attrs := []attribute.KeyValue{jsonAttr(prefix, metadata)}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		name := prefix + "." + key
		switch value := metadata[key].(type) {
		case string:
			attrs = append(attrs, attribute.String(name, value))
		case bool:
			attrs = append(attrs, attribute.Bool(name, value))
		case int:
			attrs = append(attrs, attribute.Int(name, value))
		case int64:
			attrs = append(attrs, attribute.Int64(name, value))
		case float64:
			attrs = append(attrs, attribute.Float64(name, value))
		default:
			attrs = append(attrs, jsonAttr(name, value))
		}
	}
	return attrs
}

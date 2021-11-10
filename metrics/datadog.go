package metrics

import (
	"context"
	"fmt"
	"github.com/DataDog/datadog-go/v5/statsd"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"time"
)

const (
	ddMetricTimestampKey = "metricTimestamp"
	ddMetricMethodKey    = "ddMetricMethodKey"
	metricStatusCodeKey  = "metricStatusCodeKey"
	metricAgentKey       = "metricUserAgentKey"
	tagAWSRegion         = "aws_region"
	tagStatusCode        = "status_code"
	defaultRate          = 1
)

func AddDatadogMiddleware(awsCfg *aws.Config, ddstatsd statsd.ClientInterface) {
	tm := datadogMiddleware{ddClient: ddstatsd}
	awsCfg.APIOptions = append(awsCfg.APIOptions, tm.initTraceMiddleware, tm.startTraceMiddleware, tm.deserializeTraceMiddleware)
}

type datadogMiddleware struct {
	ddClient statsd.ClientInterface
}

func (mw *datadogMiddleware) initTraceMiddleware(stack *middleware.Stack) error {
	return stack.Initialize.Add(middleware.InitializeMiddlewareFunc("InitMetricsMiddleware", func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (out middleware.InitializeOutput, metadata middleware.Metadata, err error) {
		ctx = context.WithValue(ctx, ddMetricTimestampKey, time.Now())
		return next.HandleInitialize(ctx, in)
	}), middleware.Before)
}

func (mw *datadogMiddleware) startTraceMiddleware(stack *middleware.Stack) error {
	return stack.Initialize.Add(middleware.InitializeMiddlewareFunc("StartMetricsMiddleware", func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (out middleware.InitializeOutput, metadata middleware.Metadata, err error) {
		region := awsmiddleware.GetRegion(ctx)
		metricName := fmt.Sprintf("aws.%s.%s", awsmiddleware.GetServiceID(ctx), awsmiddleware.GetOperationName(ctx))
		out, metadata, err = next.HandleInitialize(ctx, in)
		endTime := time.Now()
		tags := []string{
			fmt.Sprintf("%s:%s", tagAWSRegion, region),
			fmt.Sprintf("%s:%d", tagStatusCode, ctx.Value(metricAgentKey).(int)),
		}
		startTime := ctx.Value(ddMetricTimestampKey).(time.Time)
		val := endTime.Sub(startTime)
		err = mw.ddClient.Timing(metricName, val, tags, defaultRate)
		return out, metadata, err
	}), middleware.After)
}

func (mw *datadogMiddleware) deserializeTraceMiddleware(stack *middleware.Stack) error {
	return stack.Deserialize.Add(middleware.DeserializeMiddlewareFunc("DeserializeTraceMiddleware", func(ctx context.Context, in middleware.DeserializeInput, next middleware.DeserializeHandler) (out middleware.DeserializeOutput, metadata middleware.Metadata, err error) {
		if req, ok := in.Request.(*smithyhttp.Request); ok {
			ctx = context.WithValue(ctx, ddMetricMethodKey, req.Method)
			ctx = context.WithValue(ctx, metricAgentKey, req.Header.Get("User-Agent"))
		}
		out, metadata, err = next.HandleDeserialize(ctx, in)
		if res, ok := out.RawResponse.(*smithyhttp.Response); ok {
			ctx = context.WithValue(ctx, metricStatusCodeKey, res.StatusCode)
		}
		return out, metadata, err
	}), middleware.Before)
}

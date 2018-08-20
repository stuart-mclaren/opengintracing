package main

import (
	"fmt"
	//"github.com/gin-contrib/opengintracing"
	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
	"github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/zipkin"
	"net/http"
)

type transport struct {
	underlyingTransport http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	span := opentracing.SpanFromContext(req.Context())
	if span != nil {
		opentracing.GlobalTracer().Inject(
			span.Context(),
			opentracing.HTTPHeaders,
			opentracing.HTTPHeadersCarrier(req.Header))
	}
	return t.underlyingTransport.RoundTrip(req)
}

func main() {
	// Configure tracing
	propagator := zipkin.NewZipkinB3HTTPHeaderPropagator()
	trace, closer := jaeger.NewTracer(
		"api_gateway",
		jaeger.NewConstSampler(true),
		jaeger.NewNullReporter(),
		jaeger.TracerOptions.Injector(opentracing.HTTPHeaders, propagator),
		jaeger.TracerOptions.Extractor(opentracing.HTTPHeaders, propagator),
		jaeger.TracerOptions.ZipkinSharedRPCSpan(true),
	)
	defer closer.Close()
	opentracing.SetGlobalTracer(trace)
	/*
		var fn opengintracing.ParentSpanReferenceFunc
		fn = func(sc opentracing.SpanContext) opentracing.StartSpanOption {
			return opentracing.ChildOf(sc)
		}
	*/

	// Set up routes
	r := gin.Default()
	r.Use(TracingMiddleware2())
	r.POST("",
		handler)
	r.Run(":8001")
}

func printHeaders(message string, header http.Header) {
	fmt.Println(message)
	for k, v := range header {
		fmt.Printf("%s: %s\n", k, v)
	}
}

func handler(c *gin.Context) {
	span := opentracing.SpanFromContext(c.Request.Context())
	if span == nil {
		fmt.Println("Span not found")
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	req, _ := http.NewRequest("POST", "http://localhost:8003", nil)

	printHeaders("Incoming headers", c.Request.Header)
	printHeaders("Outgoing headers", req.Header)

	client := http.Client{Transport: &transport{underlyingTransport: http.DefaultTransport}}
    // Propagate incoming request context (eg to propagate tracing headers)
	req = req.WithContext(c.Request.Context())
	resp, err := client.Do(req)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Println("Unexpected response from service3")
	}
	c.Status(http.StatusOK)
}

func TracingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// create a "root" Span from the given Context
		// and finish it when the request ends
		span, ctx := opentracing.StartSpanFromContext(c, "gin.request")
		defer span.Finish()

		// propagate the trace in the Gin Context and process the request
		c.Request = c.Request.WithContext(ctx)
		c.Next()

		// add useful tags to your Trace
		span.SetTag("http.method", c.Request.Method)
		//span.SetTag("http.status_code", strconv.Itoa(c.Writer.Status()))
		span.SetTag("http.url", c.Request.URL.Path)

		// add Datadog tag to distinguish stats for different endpoints
		span.SetTag("resource.name", c.HandlerName())
	}
}

func TracingMiddleware2() gin.HandlerFunc {
	return func(c *gin.Context) {

		// create a "root" Span from the given Context
		// and finish it when the request ends
		//span, ctx := opentracing.StartSpanFromContext(c, "gin.request")
		spanContext, err := opentracing.GlobalTracer().Extract(opentracing.HTTPHeaders,
			opentracing.HTTPHeadersCarrier(c.Request.Header))
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		/*
			fn := func(sc opentracing.SpanContext) opentracing.StartSpanOption {
				return opentracing.ChildOf(sc)
			}
		*/

		opts := append([]opentracing.StartSpanOption{opentracing.ChildOf(spanContext)})
		span, ctx := opentracing.StartSpanFromContext(c, "gin.request", opts...)
		defer span.Finish()

		// propagate the trace in the Gin Context and process the request
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

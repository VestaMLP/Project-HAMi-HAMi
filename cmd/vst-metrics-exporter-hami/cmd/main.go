package main

import (
	"flag"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
	"log"
	"net/http"
	"vst-metrics-hami-exporter/collectors"
)

var (
	addr            = flag.String("web.listen-address", ":9808", "Port to listen on for web interface and telemetry.") // 监听请求端口
	rateLimit       = flag.Int("web.rate-limit", 2, "Limit for requests per second.")                                  // 每秒请求数限制
	rateBurst       = flag.Int("web.rate-burst", 10, "Maximum per second burst rate for requests.")                    // 突发请求数限制
	metricsEndpoint = "/metrics"                                                                                       // 指标路径
)

func main() {
	// 校验采集路径
	parseAndVerifyFlags()
	// 注册指标采集器
	err := prometheus.Register(collectors.Enabled())
	if err != nil {
		log.Fatalf("collector could not be registered: %v", err)
		return
	}

	// 设置请求连接条件限制
	handlerWithMiddleware := limitRequests(
		getOnly(
			endpointOnly(
				noBody(promhttp.Handler()), metricsEndpoint)),
		rate.Limit(*rateLimit), *rateBurst)

	log.Printf("listening on %v", *addr)
	log.Fatalf("ListenAndServe error: %v", http.ListenAndServe(*addr, handlerWithMiddleware))
}

func parseAndVerifyFlags() {
	flag.Parse()
}

// 将所有响应限制为404，其中不使用传递的端点。
// 用于最小化服务器的可能输出。
func endpointOnly(next http.Handler, endpoint string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != endpoint {
			w.WriteHeader(http.StatusNotFound)
			_, err := w.Write([]byte{})
			if err != nil {
				log.Print(err)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

// 仅接收get请求
func getOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, err := w.Write([]byte{})
			if err != nil {
				log.Print(err)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

// 校验请求连接包含body(请求体)返回400
func noBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != http.NoBody {
			w.WriteHeader(http.StatusBadRequest)
			_, err := w.Write([]byte{})
			if err != nil {
				log.Print(err)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

// 为发往端点的请求设置速率限制和突发限制
func limitRequests(next http.Handler, rateLimit rate.Limit, burstLimit int) http.Handler {
	limiter := rate.NewLimiter(rateLimit, burstLimit)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

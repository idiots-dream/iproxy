package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"

	"github.com/idiots-dream/iproxy/balancer"
)

var (
	XRealIP       = http.CanonicalHeaderKey("X-Real-IP")
	XProxy        = http.CanonicalHeaderKey("X-Proxy")
	XForwardedFor = http.CanonicalHeaderKey("X-Forwarded-For")
)

var (
	ReverseProxy = "Balancer-Reverse-Proxy"
)

// HTTPProxy refers to a reverse proxy in the balancer
type HTTPProxy struct {
	hostMap map[string]*httputil.ReverseProxy
	lb      balancer.Balancer

	sync.RWMutex // protect alive
	alive        map[string]bool
}

// NewHTTPProxy create new reverse(反向，撤销) proxy with url and balancer algorithm
func NewHTTPProxy(targetHosts []string, algorithm string) (*HTTPProxy, error) {
	hosts := make([]string, 0)
	hostMap := make(map[string]*httputil.ReverseProxy)
	alive := make(map[string]bool)
	for _, targetHost := range targetHosts {
		log.Println("target Host:", targetHost)
		url, err := url.Parse(targetHost)
		log.Println("url:", url)
		if err != nil {
			return nil, err
		}
		proxy := httputil.NewSingleHostReverseProxy(url)
		log.Println("proxy:", proxy)

		originDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originDirector(req)
			req.Header.Set(XProxy, ReverseProxy)
			req.Header.Set(XRealIP, GetIP(req))
		}

		host := GetHost(url)
		alive[host] = true // initial mark alive
		hostMap[host] = proxy
		hosts = append(hosts, host)
	}

	lb, err := balancer.Build(algorithm, hosts)
	if err != nil {
		return nil, err
	}

	return &HTTPProxy{
		hostMap: hostMap,
		lb:      lb,
		alive:   alive,
	}, nil
}

// ServeHTTP implements a proxy to the http server
func (h *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("proxy causes panic :%s", err)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(err.(error).Error()))
		}
	}()

	host, err := h.lb.Balance(GetIP(r))
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(fmt.Sprintf("balance error: %s", err.Error())))
		return
	}

	h.lb.Inc(host)
	defer h.lb.Done(host)
	h.hostMap[host].ServeHTTP(w, r)
}

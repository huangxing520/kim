package router

import (
	"context"
	"net/http"
	"path"

	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/naming"
	"github.com/klintcheng/kim/services/router/conf"
	"github.com/klintcheng/kim/services/router/handler"
	"github.com/klintcheng/kim/services/router/ipregion"
)

type Server struct {
	config   *Config
	dataPath string
	srv      *http.Server
	naming   naming.Naming
	log      *logger.Logger
}

func New(ctx context.Context, cfg *Config, dataPath string) (*Server, error) {
	log, err := logger.Init(logger.Settings{
		Level:       cfg.LogLevel,
		Filename:    "./data/router.log",
		ServiceName: "router",
		Kafka:       cfg.Kafka,
	})
	if err != nil {
		return nil, err
	}
	logger.RouterLogger = log.Sugar()
	logClosed := false
	defer func() {
		if !logClosed {
			_ = log.Close()
		}
	}()

	mappings, err := conf.LoadMapping(path.Join(dataPath, "mapping.json"))
	if err != nil {
		return nil, err
	}
	logger.RouterLogger.Infof("load mappings - %v", mappings)
	regions, err := conf.LoadRegions(path.Join(dataPath, "regions.json"))
	if err != nil {
		return nil, err
	}
	logger.RouterLogger.Infof("load regions - %v", regions)

	region, err := ipregion.NewIp2region(path.Join(dataPath, "ip2region.db"))
	if err != nil {
		return nil, err
	}

	ns, err := naming.NewNaming(cfg.ConsulURL)
	if err != nil {
		return nil, err
	}

	routerAPI := &handler.RouterApi{
		Naming:   ns,
		IpRegion: region,
		Config: conf.Router{
			Mapping: mappings,
			Regions: regions,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/api/lookup/{token}", routerAPI.Lookup)

	srv := &http.Server{
		Addr:    cfg.Listen,
		Handler: mux,
	}

	logClosed = true

	return &Server{
		config:   cfg,
		dataPath: dataPath,
		srv:      srv,
		naming:   ns,
		log:      log,
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	logger.RouterLogger.Infof("router service starting on %s", s.config.Listen)
	return s.srv.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	err := s.srv.Shutdown(ctx)
	if s.log != nil {
		_ = s.log.Close()
	}
	return err
}

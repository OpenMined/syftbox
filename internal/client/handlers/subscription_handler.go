package handlers

import (
	"errors"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
	"github.com/openmined/syftbox/internal/client/subscriptions"
)

type SubscriptionHandler struct {
	datasiteMgr *datasitemgr.DatasiteManager
}

func NewSubscriptionHandler(datasiteMgr *datasitemgr.DatasiteManager) *SubscriptionHandler {
	return &SubscriptionHandler{datasiteMgr: datasiteMgr}
}

// Get subscriptions config.
func (h *SubscriptionHandler) Get(c *gin.Context) {
	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	path := filepath.Join(ds.GetWorkspace().MetadataDir, subscriptions.FileName)
	cfg, err := subscriptions.Load(path)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, ErrCodeUnknownError, err)
		return
	}

	c.JSON(http.StatusOK, SubscriptionsResponse{
		Path:   path,
		Config: toSubscriptionConfig(cfg),
	})
}

// Update subscriptions config.
func (h *SubscriptionHandler) Update(c *gin.Context) {
	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	var req SubscriptionsUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		AbortWithError(c, http.StatusBadRequest, ErrCodeBadRequest, err)
		return
	}

	cfg, err := fromSubscriptionConfig(req.Config)
	if err != nil {
		AbortWithError(c, http.StatusBadRequest, ErrCodeBadRequest, err)
		return
	}

	path := filepath.Join(ds.GetWorkspace().MetadataDir, subscriptions.FileName)
	if err := subscriptions.Save(path, cfg); err != nil {
		AbortWithError(c, http.StatusInternalServerError, ErrCodeUnknownError, err)
		return
	}

	ds.GetSyncManager().TriggerSync()

	c.JSON(http.StatusOK, SubscriptionsResponse{
		Path:   path,
		Config: toSubscriptionConfig(cfg),
	})
}

// Discovery returns metadata for accessible files that are not currently allowed by subscriptions.
func (h *SubscriptionHandler) Discovery(c *gin.Context) {
	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	path := filepath.Join(ds.GetWorkspace().MetadataDir, subscriptions.FileName)
	cfg, err := subscriptions.Load(path)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, ErrCodeUnknownError, err)
		return
	}

	view, err := ds.GetSDK().Datasite.GetView(c.Request.Context(), nil)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, ErrCodeUnknownError, err)
		return
	}

	files := make([]DiscoveryFile, 0)
	for _, file := range view.Files {
		if aclspec.IsACLFile(file.Key) || subscriptions.IsSubFile(file.Key) {
			continue
		}
		action := cfg.ActionForPath(ds.GetWorkspace().Owner, file.Key)
		if action == subscriptions.ActionAllow {
			continue
		}
		files = append(files, DiscoveryFile{
			Path:         file.Key,
			ETag:         file.ETag,
			Size:         file.Size,
			LastModified: file.LastModified,
			Action:       string(action),
		})
	}

	c.JSON(http.StatusOK, DiscoveryResponse{Files: files})
}

func toSubscriptionConfig(cfg *subscriptions.Config) SubscriptionConfig {
	if cfg == nil {
		cfg = subscriptions.DefaultConfig()
	}

	out := SubscriptionConfig{
		Version:  cfg.Version,
		Defaults: map[string]string{"action": string(cfg.Defaults.Action)},
		Rules:    make([]SubscriptionRule, 0, len(cfg.Rules)),
	}

	for _, rule := range cfg.Rules {
		out.Rules = append(out.Rules, SubscriptionRule{
			Action:   string(rule.Action),
			Datasite: rule.Datasite,
			Path:     rule.Path,
		})
	}

	return out
}

func fromSubscriptionConfig(cfg SubscriptionConfig) (*subscriptions.Config, error) {
	out := &subscriptions.Config{
		Version: cfg.Version,
		Defaults: subscriptions.Defaults{
			Action: subscriptions.ActionBlock,
		},
		Rules: make([]subscriptions.Rule, 0, len(cfg.Rules)),
	}

	if action, ok := cfg.Defaults["action"]; ok {
		parsed, err := parseSubAction(action)
		if err != nil {
			return nil, err
		}
		out.Defaults.Action = parsed
	}

	for _, rule := range cfg.Rules {
		parsed, err := parseSubAction(rule.Action)
		if err != nil {
			return nil, err
		}
		out.Rules = append(out.Rules, subscriptions.Rule{
			Action:   parsed,
			Datasite: rule.Datasite,
			Path:     rule.Path,
		})
	}

	if out.Version == 0 {
		out.Version = 1
	}

	return out, nil
}

func parseSubAction(raw string) (subscriptions.Action, error) {
	action, err := subscriptions.LoadAction(raw)
	if err != nil {
		return "", err
	}
	return action, nil
}

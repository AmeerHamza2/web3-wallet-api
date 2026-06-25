package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// healthResponse is the liveness/readiness body.
type healthResponse struct {
	Status    string `json:"status" example:"ok"`
	RPCOnline bool   `json:"rpc_online" example:"true"`
}

// Healthz godoc
// @Summary      Liveness probe
// @Description  Always returns 200 if the process is running.
// @Tags         system
// @Produce      json
// @Success      200 {object} healthResponse
// @Router       /healthz [get]
func (h *Handler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, healthResponse{Status: "ok", RPCOnline: h.eth.Connected()})
}

// Readyz godoc
// @Summary      Readiness probe
// @Description  Returns 200 when the service can serve traffic. RPC connectivity is reported but not required (offline features remain available).
// @Tags         system
// @Produce      json
// @Success      200 {object} healthResponse
// @Router       /readyz [get]
func (h *Handler) Readyz(c *gin.Context) {
	c.JSON(http.StatusOK, healthResponse{Status: "ready", RPCOnline: h.eth.Connected()})
}

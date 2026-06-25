// Package api assembles the HTTP router, wiring middleware chains to handlers.
package api

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/AmeerHamza2/web3-wallet-api/docs" // generated Swagger spec
	"github.com/AmeerHamza2/web3-wallet-api/internal/api/handlers"
	"github.com/AmeerHamza2/web3-wallet-api/internal/api/middleware"
	"github.com/AmeerHamza2/web3-wallet-api/internal/auth"
)

// Options configures the router.
type Options struct {
	Handler    *handlers.Handler
	Issuer     *auth.Issuer
	Logger     *slog.Logger
	Production bool
}

// NewRouter builds the fully-wired gin engine.
//
// @title           Web3 Wallet API
// @version         1.0
// @description     A secure, microservice-style Ethereum wallet API in Go: wallet creation, transaction signing, and balance checking on the Sepolia testnet.
// @description     Private keys are stored encrypted (Web3 Secret Storage). Endpoints are protected by JWT bearer tokens (OAuth2 client-credentials) with role-based access control.
// @BasePath        /api/v1
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
func NewRouter(opts Options) *gin.Engine {
	if opts.Production {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(
		middleware.RequestID(),
		middleware.Logger(opts.Logger),
		middleware.Recovery(opts.Logger),
	)

	h := opts.Handler

	// Unauthenticated: health probes and Swagger UI.
	r.GET("/healthz", h.Healthz)
	r.GET("/readyz", h.Readyz)
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := r.Group("/api/v1")

	// Token endpoint (credentials in body, no bearer required).
	v1.POST("/auth/token", h.IssueToken)

	// Everything below requires a valid bearer token.
	authed := v1.Group("")
	authed.Use(middleware.Authenticate(opts.Issuer))
	{
		// Any authenticated role may create a wallet, read balances, and send.
		authed.POST("/wallets", middleware.RequireRole(auth.RoleUser), h.CreateWallet)
		authed.GET("/wallets/:address/balance", middleware.RequireRole(auth.RoleUser), h.GetBalance)
		authed.POST("/transactions", middleware.RequireRole(auth.RoleUser), h.SendTransaction)

		// Listing all custodied wallets is an admin-only operation.
		authed.GET("/wallets", middleware.RequireRole(auth.RoleAdmin), h.ListWallets)
	}

	return r
}

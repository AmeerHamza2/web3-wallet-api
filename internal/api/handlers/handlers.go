// Package handlers implements the HTTP request handlers for the wallet API.
//
// Handlers are deliberately thin: they validate/parse input, delegate to the
// domain services, and map domain errors to HTTP status codes. All business
// logic lives in the service packages so it stays testable without HTTP.
package handlers

import (
	"errors"
	"math/big"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"

	"github.com/AmeerHamza2/web3-wallet-api/internal/auth"
	"github.com/AmeerHamza2/web3-wallet-api/internal/ethereum"
	"github.com/AmeerHamza2/web3-wallet-api/internal/transaction"
	"github.com/AmeerHamza2/web3-wallet-api/internal/wallet"
)

// Handler holds the dependencies shared by all route handlers.
type Handler struct {
	wallets *wallet.Service
	txs     *transaction.Service
	eth     *ethereum.Client
	issuer  *auth.Issuer
}

// New constructs a Handler.
func New(w *wallet.Service, t *transaction.Service, e *ethereum.Client, i *auth.Issuer) *Handler {
	return &Handler{wallets: w, txs: t, eth: e, issuer: i}
}

// --- DTOs (documented for Swagger) ---

// errorResponse is the uniform error envelope.
type errorResponse struct {
	Error string `json:"error" example:"invalid address"`
}

// tokenRequest is the OAuth2 client-credentials input.
type tokenRequest struct {
	ClientID     string `json:"client_id" binding:"required" example:"demo-client"`
	ClientSecret string `json:"client_secret" binding:"required" example:"dev-insecure-client-secret-change-me"`
}

// balanceResponse reports an address balance in wei and ether.
type balanceResponse struct {
	Address    string `json:"address" example:"0x71C7656EC7ab88b098defB751B7401B5f6d8976F"`
	BalanceWei string `json:"balance_wei" example:"1000000000000000000"`
	BalanceEth string `json:"balance_eth" example:"1.000000000000000000"`
}

// sendRequest is the body for POST /transactions.
type sendRequest struct {
	From     string `json:"from" binding:"required" example:"0x71C7656EC7ab88b098defB751B7401B5f6d8976F"`
	To       string `json:"to" binding:"required" example:"0x71C7656EC7ab88b098defB751B7401B5f6d8976F"`
	ValueWei string `json:"value_wei" binding:"required" example:"1000000000000000"`
	GasLimit uint64 `json:"gas_limit,omitempty" example:"21000"`
}

// IssueToken godoc
// @Summary      Issue an access token (OAuth2 client credentials)
// @Description  Exchanges client_id/client_secret for a short-lived JWT bearer token.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request body tokenRequest true "Client credentials"
// @Success      200 {object} auth.Token
// @Failure      400 {object} errorResponse
// @Failure      401 {object} errorResponse
// @Router       /auth/token [post]
func (h *Handler) IssueToken(c *gin.Context) {
	var req tokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	tok, err := h.issuer.IssueForClient(req.ClientID, req.ClientSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, errorResponse{Error: "invalid client credentials"})
		return
	}
	c.JSON(http.StatusOK, tok)
}

// CreateWallet godoc
// @Summary      Create a new wallet
// @Description  Generates a new Ethereum account; the private key is stored encrypted (Web3 Secret Storage) and never returned.
// @Tags         wallets
// @Produce      json
// @Security     BearerAuth
// @Success      201 {object} wallet.Wallet
// @Failure      401 {object} errorResponse
// @Failure      500 {object} errorResponse
// @Router       /wallets [post]
func (h *Handler) CreateWallet(c *gin.Context) {
	w, err := h.wallets.Create()
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "failed to create wallet"})
		return
	}
	c.JSON(http.StatusCreated, w)
}

// ListWallets godoc
// @Summary      List managed wallets
// @Description  Returns all accounts held by the service. Requires the admin role.
// @Tags         wallets
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array} wallet.Wallet
// @Failure      401 {object} errorResponse
// @Failure      403 {object} errorResponse
// @Router       /wallets [get]
func (h *Handler) ListWallets(c *gin.Context) {
	c.JSON(http.StatusOK, h.wallets.List())
}

// GetBalance godoc
// @Summary      Get an address balance
// @Description  Reads the latest on-chain balance for any address. Requires RPC connectivity.
// @Tags         wallets
// @Produce      json
// @Security     BearerAuth
// @Param        address path string true "Ethereum address (0x...)"
// @Success      200 {object} balanceResponse
// @Failure      400 {object} errorResponse
// @Failure      401 {object} errorResponse
// @Failure      503 {object} errorResponse
// @Router       /wallets/{address}/balance [get]
func (h *Handler) GetBalance(c *gin.Context) {
	address := c.Param("address")
	if !common.IsHexAddress(address) {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid address"})
		return
	}
	bal, err := h.eth.BalanceAt(c.Request.Context(), common.HexToAddress(address))
	if err != nil {
		if errors.Is(err, ethereum.ErrNotConnected) {
			c.JSON(http.StatusServiceUnavailable, errorResponse{Error: "ethereum node unavailable"})
			return
		}
		c.JSON(http.StatusBadGateway, errorResponse{Error: "failed to fetch balance"})
		return
	}
	c.JSON(http.StatusOK, balanceResponse{
		Address:    address,
		BalanceWei: bal.String(),
		BalanceEth: weiToEther(bal),
	})
}

// SendTransaction godoc
// @Summary      Send a value transfer
// @Description  Builds, signs, and broadcasts an ETH transfer from a managed wallet. Requires RPC connectivity.
// @Tags         transactions
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body sendRequest true "Transfer parameters"
// @Success      202 {object} transaction.Receipt
// @Failure      400 {object} errorResponse
// @Failure      401 {object} errorResponse
// @Failure      503 {object} errorResponse
// @Router       /transactions [post]
func (h *Handler) SendTransaction(c *gin.Context) {
	var req sendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	receipt, err := h.txs.Send(c.Request.Context(), transaction.SendRequest{
		From:     req.From,
		To:       req.To,
		ValueWei: req.ValueWei,
		GasLimit: req.GasLimit,
	})
	if err != nil {
		switch {
		case errors.Is(err, transaction.ErrInvalidAddress),
			errors.Is(err, transaction.ErrInvalidAmount),
			errors.Is(err, transaction.ErrUnknownSender),
			errors.Is(err, wallet.ErrWalletNotFound):
			c.JSON(http.StatusBadRequest, errorResponse{Error: err.Error()})
		case errors.Is(err, ethereum.ErrNotConnected):
			c.JSON(http.StatusServiceUnavailable, errorResponse{Error: "ethereum node unavailable"})
		default:
			c.JSON(http.StatusBadGateway, errorResponse{Error: "failed to send transaction"})
		}
		return
	}
	c.JSON(http.StatusAccepted, receipt)
}

// weiToEther formats a wei amount as a fixed-point ether string (18 decimals)
// without floating-point error.
func weiToEther(wei *big.Int) string {
	if wei == nil {
		return "0"
	}
	ether := new(big.Rat).SetFrac(wei, big.NewInt(1e18))
	return ether.FloatString(18)
}

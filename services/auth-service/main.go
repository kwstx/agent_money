package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/galan/agent_money/services/auth-service/internal/db"
	"github.com/galan/agent_money/services/auth-service/internal/models"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

type Server struct {
	repo *db.Repository
}

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgresql://postgres:postgres@localhost:5432/agent_money?sslmode=disable"
	}

	conn, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	server := &Server{
		repo: db.NewRepository(conn),
	}

	r := mux.NewRouter()

	// Admin API
	r.HandleFunc("/admin/orgs", server.CreateOrg).Methods("POST")
	r.HandleFunc("/admin/agents", server.CreateAgent).Methods("POST")
	r.HandleFunc("/admin/policies", server.CreatePolicy).Methods("POST")
	r.HandleFunc("/admin/approvals/{id}/decide", server.DecideApproval).Methods("POST")

	// Internal Policy Query API (for Policy Engine)
	r.HandleFunc("/internal/effective-policy", server.GetEffectivePolicy).Methods("GET")

	// OIDC/OAuth Callback (Integration Simulation)
	r.HandleFunc("/auth/callback", server.OAuthCallback).Methods("GET")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	fmt.Printf("Auth Service starting on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func (s *Server) CreateOrg(w http.ResponseWriter, r *http.Request) {
	var org models.Organization
	if err := json.NewDecoder(r.Body).Decode(&org); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.repo.CreateOrganization(&org); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(org)
}

func (s *Server) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var agent models.Agent
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.repo.CreateAgent(&agent); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(agent)
}

func (s *Server) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	var policy models.Policy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.repo.CreatePolicy(&policy); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(policy)
}

func (s *Server) DecideApproval(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := uuid.Parse(vars["id"])
	
	var req struct {
		ApproverID uuid.UUID `json:"approver_id"`
		Status     string    `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.repo.DecideApproval(id, req.ApproverID, req.Status); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) GetEffectivePolicy(w http.ResponseWriter, r *http.Request) {
	agentIDStr := r.URL.Query().Get("agent_id")
	workflowIDStr := r.URL.Query().Get("workflow_id")

	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		http.Error(w, "invalid agent_id", http.StatusBadRequest)
		return
	}

	var workflowID *uuid.UUID
	if workflowIDStr != "" {
		id, err := uuid.Parse(workflowIDStr)
		if err == nil {
			workflowID = &id
		}
	}

	policy, err := s.repo.GetEffectivePolicy(agentID, workflowID)
	if err != nil {
		http.Error(w, "policy not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(policy)
}

func (s *Server) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	// Simulate external OIDC integration
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	fmt.Printf("Received OAuth callback: code=%s, state=%s\n", code, state)
	
	// In a real scenario, we would exchange the code for a token,
	// fetch user info, and link it to an Organization.
	
	w.Write([]byte("External identity integrated successfully"))
}

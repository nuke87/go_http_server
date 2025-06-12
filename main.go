package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/nuke87/go_http_server/internal/database"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
}

func main() {
	const filepathRoot = "."
	const port = "8080"

	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		log.Fatal("DB_URL must be set")
	}
	platform := os.Getenv("PLATFORM")

	dbConn, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Error opening database: %s", err)
	}
	dbQueries := database.New(dbConn)

	apiCfg := apiConfig{
		fileserverHits: atomic.Int32{},
		db:             dbQueries,
		platform:       platform,
	}

	mux := http.NewServeMux()
	fsHandler := apiCfg.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(filepathRoot))))
	mux.Handle("/app/", fsHandler)

	mux.HandleFunc("GET /api/healthz", handlerReadiness)
	//mux.HandleFunc("POST /api/validate_chirp", handlerChirpsValidate)
	mux.HandleFunc("POST /api/users", apiCfg.handlerCreateUser)
	mux.HandleFunc("POST /admin/reset", apiCfg.handlerReset)
	mux.HandleFunc("GET /admin/metrics", apiCfg.handlerMetrics)
	mux.HandleFunc("POST /api/chirps", apiCfg.handlerCreateChirp)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	log.Printf("Serving on port: %s\n", port)
	log.Fatal(srv.ListenAndServe())
}

// Middleware: Zählt Zugriffe auf /app/
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

// Handler für /api/healthz
func handlerReadiness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// Handler für /api/validate_chirp (Dummy)
func handlerChirpsValidate(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"valid":true}`))
}

// Handler für /admin/reset
// Setzt den Zugriffszähler zurück und löscht (optional, siehe Aufgabe) alle User aus der Datenbank.
// In dieser Version wird nur der Zähler zurückgesetzt.
func (cfg *apiConfig) handlerReset(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// Handler für /admin/metrics
// Gibt eine HTML-Seite mit der aktuellen Anzahl der Zugriffe auf /app/ zurück.
func (cfg *apiConfig) handlerMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, cfg.fileserverHits.Load())))
}

// User struct für die JSON-Antwort
type User struct {
	ID        uuid.UUID `json:"id"`         // Eindeutige User-ID (UUID), wird als "id" im JSON ausgegeben
	CreatedAt time.Time `json:"created_at"` // Erstellungszeitpunkt, wird als "created_at" im JSON ausgegeben
	UpdatedAt time.Time `json:"updated_at"` // Zeitpunkt der letzten Änderung, wird als "updated_at" im JSON ausgegeben
	Email     string    `json:"email"`      // E-Mail-Adresse des Users, wird als "email" im JSON ausgegeben
}

// Handler für /api/users (POST)
func (cfg *apiConfig) handlerCreateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { // Nur POST-Anfragen sind erlaubt
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed) // Bei anderen Methoden: 405 zurückgeben
		return
	}

	type requestBody struct {
		Email string `json:"email"` // Erwartet ein Feld "email" im JSON-Request
	}
	var req requestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" { // JSON dekodieren und prüfen, ob E-Mail vorhanden ist
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest) // Fehlerhafte Anfrage: 400 zurückgeben
		return
	}

	// Dummy-User anlegen (in echter App: DB nutzen)
	now := time.Now().UTC() // Aktuelle Zeit in UTC holen
	user := User{
		ID:        uuid.New(), // Neue UUID generieren
		CreatedAt: now,        // Erstellungszeitpunkt setzen
		UpdatedAt: now,        // Aktualisierungszeitpunkt setzen
		Email:     req.Email,  // E-Mail aus Request übernehmen
	}

	w.Header().Set("Content-Type", "application/json") // Antwort als JSON deklarieren
	w.WriteHeader(http.StatusCreated)                  // HTTP-Status 201 Created setzen
	json.NewEncoder(w).Encode(user)                    // User-Objekt als JSON zurückgeben
}

// Handler für /api/chirps (POST)
// Erwartet JSON {"body": "...", "user_id": "..."}.
// Prüft die Länge und ersetzt ggf. "böse" Wörter. Speichert das Chirp in der DB und gibt es als JSON zurück.
func (cfg *apiConfig) handlerCreateChirp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	type requestBody struct {
		Body   string    `json:"body"`
		UserID uuid.UUID `json:"user_id"`
	}
	type errorResponse struct {
		Error string `json:"error"`
	}
	type chirpResponse struct {
		ID        uuid.UUID `json:"id"`
		Body      string    `json:"body"`
		UserID    uuid.UUID `json:"user_id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	var req requestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" || req.UserID == uuid.Nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if len(req.Body) > 140 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse{Error: "Chirp is too long"})
		return
	}

	// Profanity-Filter anwenden
	badWords := map[string]struct{}{
		"kerfuffle": {},
		"sharbert":  {},
		"fornax":    {},
	}
	words := strings.Split(req.Body, " ")
	for i, word := range words {
		if _, found := badWords[strings.ToLower(word)]; found {
			words[i] = "****"
		}
	}
	cleanedBody := strings.Join(words, " ")

	// Chirp in der Datenbank speichern
	id := uuid.New()
	now := time.Now().UTC()
	chirp, err := cfg.db.CreateChirp(r.Context(), database.CreateChirpParams{
		ID:        id,
		CreatedAt: now,
		UpdatedAt: now,
		Body:      cleanedBody,
		UserID:    req.UserID,
	})
	if err != nil {
		http.Error(w, `{"error":"could not create chirp"}`, http.StatusInternalServerError)
		return
	}

	// Chirp als JSON zurückgeben
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(chirpResponse{
		ID:        chirp.ID,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
	})
}

/*
Dokumentation:
--------------
handlerReset:
  - Setzt den Zugriffszähler (fileserverHits) auf 0 zurück.
  - Antwort: HTTP 200 OK, Body: "OK"
  - In einer erweiterten Version kann hier auch das Löschen aller User aus der Datenbank erfolgen,
    z.B. durch Aufruf von cfg.db.DeleteAllUsers(r.Context()), wenn PLATFORM == "dev".

handlerMetrics:
  - Gibt eine HTML-Seite mit der aktuellen Anzahl der Zugriffe auf /app/ zurück.
  - Antwort: HTTP 200 OK, Content-Type: text/html
  - Die Zahl wird aus cfg.fileserverHits.Load() gelesen.
*/

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spiffe/go-spiffe/v2/spiffeid"

	"github.com/cerbos/cerbos-sdk-go/cerbos"
)

type Document struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Owner    string `json:"owner"`
}

var documents = map[string]Document{
	"doc1": {
		ID:       "doc1",
		Title:    "Bat mobile blue prints",
		Category: "top_secret",
		Owner:    "Lucius Fox",
	},
	"doc2": {
		ID:       "doc2",
		Title:    "Friday bowling league",
		Category: "internal",
		Owner:    "Joe Bloggs",
	},
	"doc3": {
		ID:       "doc3",
		Title:    "Press release: Gotham marathon",
		Category: "public",
		Owner:    "Jane Barton",
	},
}

func main() {
	ctx, stopFunc := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer func() {
		stopFunc()
	}()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := startServer(ctx); err != nil {
		slog.Error("Server error", "error", err)
	}
}

func getCallerID(r *http.Request) (spiffeid.ID, error) {
	xfcc := r.Header.Get("X-Forwarded-Client-Cert")
	if xfcc == "" {
		return spiffeid.ID{}, errors.New("empty XFCC header")
	}

	for segment := range strings.SplitSeq(xfcc, ";") {
		if strings.HasPrefix(segment, "URI=") {
			uriStr := strings.TrimPrefix(segment, "URI=")
			uri, err := url.Parse(uriStr)
			if err != nil {
				return spiffeid.ID{}, fmt.Errorf("failed to parse XFCC URI: %w", err)
			}

			id, err := spiffeid.FromURI(uri)
			if err != nil {
				return spiffeid.ID{}, fmt.Errorf("failed to parse SPIFFE ID from URI: %w", err)
			}

			return id, nil
		}
	}

	return spiffeid.ID{}, errors.New("unable to obtain SPIFFE ID from request")
}

func startServer(ctx context.Context) error {
	cerbosClient, err := cerbos.New("dns:///cerbos.cerbos.svc.cluster.local:3593", cerbos.WithPlaintext())
	if err != nil {
		slog.Error("Failed to create cerbos client", "error", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/docs/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := getCallerID(r)
		if err != nil {
			slog.Error("Error obtaining SPIFFE ID", "error", err)
			w.WriteHeader(http.StatusForbidden)
			return
		}

		logger := slog.With("caller", id.String())
		logger.Info("Checking permissions with Cerbos")

		docID := r.PathValue("id")
		doc, exists := documents[docID]
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var action string
		switch r.Method {
		case http.MethodGet:
			action = "read"
		default:
			action = "modify"
		}

		allowed, err := cerbosClient.IsAllowed(r.Context(),
			cerbos.NewPrincipal(id.String(), "api").WithAttr("trustDomain", id.TrustDomain().Name()),
			cerbos.NewResource("document", docID).WithAttr("category", doc.Category),
			action,
		)
		if err != nil {
			slog.Error("Cerbos request failed", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if allowed {
			slog.Info("Request allowed")
			encoder := json.NewEncoder(w)
			if err := encoder.Encode(doc); err != nil {
				slog.Error("Failed to marshal JSON", "error", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			return
		}

		slog.Warn("Request denied")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintln(w, "Access denied")
	}))

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := getCallerID(r)
		if err != nil {
			slog.Error("Error obtaining SPIFFE ID", "error", err)
			w.WriteHeader(http.StatusForbidden)
			return
		}

		fmt.Fprintf(w, "Hello %s. Nothing to see here. Move on.\n", id.String())
	}))

	server := http.Server{Addr: ":8080", Handler: mux}
	go func() {
		if err := server.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				slog.Info("Server shut down")
			} else {
				slog.Error("Server failed", "error", err)
			}
		}
	}()

	slog.Info("Server started")
	<-ctx.Done()
	return server.Shutdown(context.Background())
}

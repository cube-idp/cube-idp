package ref

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// httpsTree reports that https serves single files only. One URL names one
// document; a tree needs an archive format, and choosing one is a design
// gate rather than a guess made here.
func httpsTree(_ context.Context, p parsed) (ResolvedTree, error) {
	return ResolvedTree{}, newModeMismatchError(p.ref, ModeTree, ModeFile)
}

// httpsFile fetches one document over https.
func httpsFile(ctx context.Context, p parsed) (ResolvedFile, error) {
	return fetchHTTPS(ctx, p, &http.Client{})
}

// fetchHTTPS performs the fetch with an explicit client. The client is a
// parameter, not package state, so a test can hand in the one an
// httptest.Server publishes for its own certificate: a real transport
// against a real server, which is what lets this backend be exercised
// without ever mocking the HTTP layer. The request carries ctx, so
// cancelling the caller's context cancels the fetch in flight.
func fetchHTTPS(ctx context.Context, p parsed, client *http.Client) (ResolvedFile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.location, nil)
	if err != nil {
		return ResolvedFile{}, newFetchFailedError(p.ref, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return ResolvedFile{}, newFetchFailedError(p.ref, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ResolvedFile{}, newFetchFailedError(p.ref,
			fmt.Errorf("server answered %s", resp.Status))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return ResolvedFile{}, newFetchFailedError(p.ref, err)
	}
	return ResolvedFile{
		data: data,
		pin:  Pin{Ref: p.ref, Source: p.location, Digest: fileDigest(data)},
	}, nil
}

package srv

import (
	"encoding/json"
	"net/http"
)

// maxJSONBodyBytes is the per-request limit for JSON request bodies.
// Real client requests (widget configs, page settings, feed items)
// are well under 1 MB; a 1 MB cap stops a malicious or buggy client
// from uploading arbitrarily large JSON, which would otherwise be
// held in memory by json.Decoder until the full body is read.
const maxJSONBodyBytes = 1 << 20 // 1 MiB

// decodeJSONBody reads up to maxJSONBodyBytes from r.Body and decodes
// it into out. If the body exceeds the limit, the response is set
// to 413 Request Entity Too Large and a non-nil error is returned;
// the caller should bail without writing further.
//
// Use this in place of `json.NewDecoder(r.Body).Decode(&out)` for
// any handler that accepts a JSON request body.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, out interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(out); err != nil {
		// MaxBytesReader returns a *http.MaxBytesError when the body
		// overflows the cap. Map that to 413 so clients can tell
		// they're too large vs. malformed.
		if _, ok := err.(*http.MaxBytesError); ok {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return err
		}
		return err
	}
	return nil
}

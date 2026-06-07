// SPDX-License-Identifier: Apache-2.0

// env.go — the real os.LookupEnv backing the swappable hook in
// adapter.go. Pulled into a tiny file so a test in this package can
// override lookupEnv without redeclaring the rest of the adapter.
package stdio

import "os"

// osLookup is the production env source. Tests in this package can
// monkey-patch lookupEnv (in adapter.go) to feed a synthetic
// environment.
func osLookup(key string) (string, bool) {
	return os.LookupEnv(key)
}
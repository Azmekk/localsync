package update

import "os"

// nativeStderr is used by PrintUpdateBanner.
// Kept as a package var so it's trivial to test if needed.
var nativeStderr = os.Stderr

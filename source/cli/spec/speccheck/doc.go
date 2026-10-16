// Package speccheck pins the JSON keys of a type in a golden file, so that a test fails when the
// shape a command writes with [go.vipengele.org/cli/spec.WriteJSON] changes. Adding a key is a
// compatible change and shows up as a one-line addition to the golden file; renaming, removing or
// retyping a key is not, and must increment the object's "schema" (ADR-0007). A test that pins the
// keys makes every such change visible in review instead of reaching a reader first.
//
// [AssertJSONKeys] marshals a value with encoding/json and compares the set of key paths it
// produces with the golden file. It reads only keys, never values.
//
// # Key paths
//
// The keys of nested objects are joined with ".", and an array adds "[]" to the path of its
// elements, whatever its length:
//
//	{"schema":1,"meta":{"owner":"x"},"items":[{"name":"a"},{"id":2}],"tags":["t"],"none":[],"empty":{}}
//
// produces
//
//	empty
//	items[].id
//	items[].name
//	meta.owner
//	none[]
//	schema
//	tags[]
//
// Only leaves are listed: a non-empty object contributes the paths of its keys and not its own,
// an empty object and a null contribute their own path, and an array contributes "[]" for each
// scalar element and the key paths of each object element, which are merged. A key that changes
// from a scalar to an object therefore shows up as its path missing and its children's paths
// extra.
//
// # Golden files
//
// A golden file holds one key path per line, sorted, with a trailing newline. A blank line is
// ignored. A mismatch fails the test with the paths the value has and the file does not, and the
// paths the file has and the value does not.
//
// Running the tests with the environment variable SPECCHECK_UPDATE set to 1 writes each golden
// file from the value instead of comparing, creating its directory if needed:
//
//	SPECCHECK_UPDATE=1 go test ./...
//
// The switch is an environment variable rather than a test flag so that it never collides with a
// flag the importing test binary registers itself.
package speccheck

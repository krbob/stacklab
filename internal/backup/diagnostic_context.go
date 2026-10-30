package backup

import "strings"

// Only the first positional argument is the command. Repository names and option
// values may themselves be "check" or "backup". The service supplies this context
// before adding its global flags; direct native-runner callers use the same parser.
type resticInvocation struct {
	command string
	object  string
}

func resticInvocationFromArgs(args []string) resticInvocation {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--repo", "-r", "--repository-file", "--password-file", "--password-command", "--key-hint", "--cache-dir", "--cacert", "--tls-client-cert", "--limit-upload", "--limit-download", "--retry-lock", "--stuck-request-timeout", "--option", "-o", "--compression", "--pack-size":
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		invocation := resticInvocation{command: arg}
		if arg == "cat" && i+1 < len(args) {
			invocation.object = args[i+1]
		}
		return invocation
	}
	return resticInvocation{}
}

func (invocation resticInvocation) lockInspection() bool {
	return invocation.command == "cat" && invocation.object == "lock"
}

type resticInvocationKey struct{}

type repositoryWarningKey struct{}
type repositoryWarning func(string)

// Warning text is fixed at the subprocess boundary. Provider output, filenames
// and credentials never become job events.
func (e *diagnosticEvidence) reportWarnings(callback repositoryWarning) {
	if callback == nil {
		return
	}
	if e.cacheWarning {
		callback("Restic reported a cache problem; repository verification is evaluated independently of the cache.")
	}
	if e.cleanupWarning {
		callback("Restic could not complete lock cleanup. Inspect repository locks before the next operation; do not remove a fresh or live lock.")
	}
	if e.backendNotice {
		callback("The storage adapter reported a recognized non-data notice. Repository verification is evaluated independently of this notice.")
	}
}

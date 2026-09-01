/* Generic libFuzzer harness: pipe inputs into a Hunt stdin ASAN binary.
 * Set HACKME_LF_STDIN_BIN to the fuzzupstream stdin driver path.
 * Used for L2 seed bootstrap on catalog targets without dedicated libFuzzer harnesses.
 */
#include <fcntl.h>
#include <signal.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

static const char *stdin_bin_path(void) {
	const char *p = getenv("HACKME_LF_STDIN_BIN");
	return (p && p[0]) ? p : NULL;
}

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
	const char *bin = stdin_bin_path();
	if (!bin || size == 0 || size > 65536) {
		return 0;
	}

	int pipefd[2];
	if (pipe(pipefd) != 0) {
		return 0;
	}

	pid_t pid = fork();
	if (pid < 0) {
		close(pipefd[0]);
		close(pipefd[1]);
		return 0;
	}
	if (pid == 0) {
		close(pipefd[1]);
		if (dup2(pipefd[0], STDIN_FILENO) < 0) {
			_exit(127);
		}
		close(pipefd[0]);
		int devnull = open("/dev/null", O_WRONLY);
		if (devnull >= 0) {
			dup2(devnull, STDOUT_FILENO);
			dup2(devnull, STDERR_FILENO);
			close(devnull);
		}
		signal(SIGPIPE, SIG_DFL);
		execl(bin, bin, (char *)NULL);
		_exit(127);
	}

	close(pipefd[0]);
	size_t off = 0;
	while (off < size) {
		ssize_t n = write(pipefd[1], data + off, size - off);
		if (n <= 0) {
			break;
		}
		off += (size_t)n;
	}
	close(pipefd[1]);

	int status = 0;
	if (waitpid(pid, &status, 0) < 0) {
		return 0;
	}
	if (WIFSIGNALED(status)) {
		raise(SIGABRT);
	}
	if (WIFEXITED(status) && WEXITSTATUS(status) != 0) {
		raise(SIGABRT);
	}
	return 0;
}

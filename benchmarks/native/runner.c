#define _POSIX_C_SOURCE 200809L
#ifdef __APPLE__
#define _DARWIN_C_SOURCE
#endif

#include "xinclude.h"
#include <inttypes.h>
#include <time.h>

static volatile uint64_t checksum_sink;

static uint64_t positive_number(const char *text, const char *name)
{
	char *end;
	uintmax_t value;
	const char *p;

	for (p = text; *p; p++)
		if (*p < '0' || *p > '9')
			testref_die("invalid %s: %s", name, text);
	errno = 0;
	value = strtoumax(text, &end, 10);
	if (errno || !*text || *end || !value || value > UINT64_MAX)
		testref_die("invalid %s: %s", name, text);
	return (uint64_t)value;
}

static void check_label(const char *label)
{
	const unsigned char *p = (const unsigned char *)label;
	if (!*p)
		testref_die("empty case label");
	for (; *p; p++)
		if (!((*p >= 'a' && *p <= 'z') ||
		      (*p >= 'A' && *p <= 'Z') ||
		      (*p >= '0' && *p <= '9') || *p == '_' || *p == '-'))
			testref_die("invalid case label: %s", label);
}

static mmfile_t read_file(const char *name)
{
	mmfile_t file = {0};
	long length;
	FILE *stream = fopen(name, "rb");

	if (!stream)
		testref_die("cannot open %s: %s", name, strerror(errno));
	if (fseek(stream, 0, SEEK_END) || (length = ftell(stream)) < 0 ||
	    fseek(stream, 0, SEEK_SET))
		testref_die("cannot seek %s", name);
	if ((uintmax_t)length >= SIZE_MAX)
		testref_die("file too large: %s", name);
	file.size = (size_t)length;
	file.ptr = xmalloc(file.size + 1);
	if (fread(file.ptr, 1, file.size, stream) != file.size)
		testref_die("cannot read %s", name);
	file.ptr[file.size] = '\0';
	if (fclose(stream))
		testref_die("cannot close %s", name);
	return file;
}

static uint64_t monotonic_ns(void)
{
	struct timespec now;
	if (clock_gettime(CLOCK_MONOTONIC, &now))
		testref_die("clock_gettime failed: %s", strerror(errno));
	return (uint64_t)now.tv_sec * UINT64_C(1000000000) +
		(uint64_t)now.tv_nsec;
}

static uint64_t compute_script(mmfile_t *old, mmfile_t *new)
{
	const xpparam_t params = {0};
	xdfenv_t env;
	xdchange_t *script = NULL, *change;
	uint64_t checksum = UINT64_C(14695981039346656037);
	int status;

	if (xdl_do_diff(old, new, &params, &env) < 0)
		testref_die("xdl_do_diff failed");
	status = xdl_change_compact(&env.xdf1, &env.xdf2, params.flags);
	if (status >= 0)
		status = xdl_change_compact(&env.xdf2, &env.xdf1, params.flags);
	if (status >= 0)
		status = xdl_build_script(&env, &script);
	if (status >= 0)
		for (change = script; change; change = change->next) {
			checksum = (checksum ^ (uint64_t)change->i1) *
				UINT64_C(1099511628211);
			checksum = (checksum ^ (uint64_t)change->chg1) *
				UINT64_C(1099511628211);
			checksum = (checksum ^ (uint64_t)change->i2) *
				UINT64_C(1099511628211);
			checksum = (checksum ^ (uint64_t)change->chg2) *
				UINT64_C(1099511628211);
		}
	xdl_free_script(script);
	xdl_free_env(&env);
	if (status < 0)
		testref_die("cannot build diff script");
	return checksum;
}

static void run_batch(mmfile_t *old, mmfile_t *new, uint64_t iterations)
{
	uint64_t i;
	for (i = 0; i < iterations; i++)
		checksum_sink ^= compute_script(old, new);
}

static uint64_t calibrate(mmfile_t *old, mmfile_t *new, uint64_t duration)
{
	uint64_t batch = 1, start, elapsed;
	uint64_t target = duration / 100;

	if (target < UINT64_C(100000))
		target = UINT64_C(100000);
	if (target > UINT64_C(10000000))
		target = UINT64_C(10000000);
	for (;;) {
		start = monotonic_ns();
		run_batch(old, new, batch);
		elapsed = monotonic_ns() - start;
		if (elapsed >= target)
			return batch;
		if (batch > UINT64_MAX / 2)
			testref_die("calibration iteration overflow");
		batch *= 2;
	}
}

int main(int argc, char **argv)
{
	mmfile_t old, new;
	uint64_t duration, count, sample;

	if (argc != 6) {
		fprintf(stderr, "usage: runner CASE OLD NEW DURATION_NS COUNT\n");
		return 2;
	}
	check_label(argv[1]);
	duration = positive_number(argv[4], "duration");
	count = positive_number(argv[5], "count");
	old = read_file(argv[2]);
	new = read_file(argv[3]);
	for (sample = 0; sample < count; sample++) {
		uint64_t batch, start, elapsed, iterations = 0;

		run_batch(&old, &new, 1);
		batch = calibrate(&old, &new, duration);
		start = monotonic_ns();
		do {
			if (iterations > UINT64_MAX - batch)
				testref_die("measurement iteration overflow");
			run_batch(&old, &new, batch);
			iterations += batch;
			elapsed = monotonic_ns() - start;
		} while (elapsed < duration);
		if (printf("BenchmarkCompare/%s/GitC %" PRIu64 " %.3f ns/op\n",
			   argv[1], iterations, (double)elapsed / (double)iterations) < 0)
			testref_die("cannot write stdout");
	}
	free(old.ptr);
	free(new.ptr);
	if (fflush(stdout))
		testref_die("cannot write stdout");
	return 0;
}

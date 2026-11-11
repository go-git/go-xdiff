#include "xinclude.h"

static long number(const char *text)
{
	char *end;
	long value;
	errno = 0;
	value = strtol(text, &end, 0);
	if (errno || !*text || *end || value < 0)
		testref_die("invalid nonnegative number: %s", text);
	return value;
}

static int small_number(const char *text, int maximum)
{
	long value = number(text);
	if (value > maximum)
		testref_die("number exceeds %d: %s", maximum, text);
	return value;
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
	file.size = (size_t)length;
	file.ptr = xmalloc(file.size + 1);
	if (fread(file.ptr, 1, file.size, stream) != file.size)
		testref_die("cannot read %s", name);
	file.ptr[file.size] = '\0';
	if (fclose(stream))
		testref_die("cannot close %s", name);
	return file;
}

static int output(void *unused, mmfile_t *buffers, int count)
{
	int i;
	(void)unused;
	for (i = 0; i < count; i++)
		if (fwrite(buffers[i].ptr, 1, buffers[i].size, stdout) != buffers[i].size)
			return -1;
	return 0;
}

static int hunk_range(long old_start, long old_count,
		      long new_start, long new_count, void *unused)
{
	(void)unused;
	return printf("%ld %ld %ld %ld\n", old_start, old_count,
		      new_start, new_count) < 0 ? -1 : 0;
}

static int script(mmfile_t *old, mmfile_t *new, xpparam_t *params)
{
	xdfenv_t env;
	xdchange_t *changes = NULL, *change;
	int status = -1;
	if (xdl_do_diff(old, new, params, &env) < 0)
		return -1;
	if (xdl_change_compact(&env.xdf1, &env.xdf2, params->flags) < 0 ||
	    xdl_change_compact(&env.xdf2, &env.xdf1, params->flags) < 0 ||
	    xdl_build_script(&env, &changes) < 0)
		goto done;
	for (change = changes; change; change = change->next)
		if (printf("%ld %ld %ld %ld\n", change->i1, change->chg1,
			   change->i2, change->chg2) < 0)
			goto done;
	status = 0;
done:
	xdl_free_script(changes);
	xdl_free_env(&env);
	return status;
}

static void options(xpparam_t *params, int argc, char **argv, int start)
{
	int i;
	params->anchors = xcalloc(argc - start, sizeof(*params->anchors));
	params->ignore_regex = xcalloc(argc - start, sizeof(*params->ignore_regex));
	for (i = start; i < argc; i += 2) {
		if (i + 1 == argc)
			testref_die("missing option value");
		if (!strcmp(argv[i], "--anchor")) {
			params->anchors[params->anchors_nr++] = argv[i + 1];
		} else if (!strcmp(argv[i], "--ignore")) {
			regex_t *regex = xmalloc(sizeof(*regex));
			if (regcomp(regex, argv[i + 1], REG_EXTENDED | REG_NEWLINE))
				testref_die("invalid ignore expression: %s", argv[i + 1]);
			params->ignore_regex[params->ignore_regex_nr++] = regex;
		} else {
			testref_die("unknown option: %s", argv[i]);
		}
	}
}

int main(int argc, char **argv)
{
	xpparam_t params = {0};
	mmfile_t old, new;
	int status;
	if (argc >= 8 &&
	    (!strcmp(argv[1], "diff") || !strcmp(argv[1], "ranges"))) {
		xdemitconf_t config = {0};
		xdemitcb_t callback = {0};
		params.flags = number(argv[2]);
		options(&params, argc, argv, 8);
		config.ctxlen = number(argv[3]);
		config.interhunkctxlen = number(argv[4]);
		config.flags = number(argv[5]);
		if (!strcmp(argv[1], "ranges"))
			config.hunk_func = hunk_range;
		else
			callback.out_line = output;
		old = read_file(argv[6]);
		new = read_file(argv[7]);
		status = xdl_diff(&old, &new, &params, &config, &callback);
	} else if (argc >= 5 && !strcmp(argv[1], "script")) {
		params.flags = number(argv[2]);
		options(&params, argc, argv, 5);
		old = read_file(argv[3]);
		new = read_file(argv[4]);
		status = script(&old, &new, &params);
	} else if (argc == 13 &&
		   (!strcmp(argv[1], "merge") || !strcmp(argv[1], "merge-count"))) {
		xmparam_t config = {0};
		mmfile_t base, result = {0};
		int with_count = !strcmp(argv[1], "merge-count");
		config.xpp.flags = number(argv[2]);
		config.level = small_number(argv[3], 3);
		config.favor = small_number(argv[4], 3);
		config.style = small_number(argv[5], 2);
		config.marker_size = small_number(argv[6], INT_MAX);
		config.ancestor = *argv[7] ? argv[7] : NULL;
		config.file1 = *argv[8] ? argv[8] : NULL;
		config.file2 = *argv[9] ? argv[9] : NULL;
		base = read_file(argv[10]);
		old = read_file(argv[11]);
		new = read_file(argv[12]);
		status = xdl_merge(&base, &old, &new, &config, &result);
		free(base.ptr);
		if (status >= 0 && with_count && printf("%d\n", status) < 0)
			status = -1;
		if (status >= 0 && fwrite(result.ptr, 1, result.size, stdout) != result.size)
			status = -1;
		free(result.ptr);
		if (status >= 0 && with_count)
			status = 0;
	} else {
		fprintf(stderr,
			"usage:\n"
			"  oracle diff FLAGS CONTEXT INTERHUNK EMIT_FLAGS OLD NEW\n"
			"  oracle ranges FLAGS CONTEXT INTERHUNK EMIT_FLAGS OLD NEW\n"
			"  oracle script FLAGS OLD NEW\n"
			"  oracle merge FLAGS LEVEL FAVOR STYLE MARKER BASE_LABEL OURS_LABEL THEIRS_LABEL BASE OURS THEIRS\n");
		return 2;
	}
	free(old.ptr);
	free(new.ptr);
	for (size_t i = 0; i < params.ignore_regex_nr; i++) {
		regfree(params.ignore_regex[i]);
		free(params.ignore_regex[i]);
	}
	free(params.ignore_regex);
	free(params.anchors);
	if (status < 0)
		testref_die("xdiff operation failed");
	if (fflush(stdout))
		testref_die("cannot write stdout");
	return status > 0 ? 1 : 0;
}

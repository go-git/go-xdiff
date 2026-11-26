#ifndef TESTREF_GIT_COMPAT_UTIL_H
#define TESTREF_GIT_COMPAT_UTIL_H

#include <ctype.h>
#include <errno.h>
#include <limits.h>
#include <regex.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* Match Git's sane-ctype.h and ctype.c, not the locale-dependent libc tables.
 * Git treats VT and FF as control characters, but not as whitespace.
 */
#undef isascii
#undef isspace
#undef isdigit
#undef isalpha
#undef isalnum
#undef isprint
#undef islower
#undef isupper
#undef tolower
#undef toupper
#undef iscntrl
#undef ispunct
#undef isxdigit

static inline int testref_isdigit(unsigned char c)
{
	return c >= '0' && c <= '9';
}

static inline int testref_isalpha(unsigned char c)
{
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z');
}

static inline int testref_isspace(unsigned char c)
{
	return c == ' ' || c == '\t' || c == '\n' || c == '\r';
}

static inline int testref_ispunct(unsigned char c)
{
	return c >= '!' && c <= '~' &&
		!testref_isalpha(c) && !testref_isdigit(c);
}

static inline int testref_isalnum(unsigned char c)
{
	return testref_isalpha(c) || testref_isdigit(c);
}

static inline int testref_iscase(int c, int lower)
{
	return testref_isalpha((unsigned char)c) &&
		((c & 0x20) != 0) == lower;
}

static inline int testref_case(unsigned char c, int high)
{
	return testref_isalpha(c) ? (c & ~0x20) | high : c;
}

static inline int testref_iscntrl(unsigned char c)
{
	return c < 0x20 || c == 0x7f;
}

static inline int testref_isxdigit(unsigned char c)
{
	return testref_isdigit(c) ||
		(c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F');
}

#define isascii(c) (((c) & ~0x7f) == 0)
#define isspace(c) testref_isspace((unsigned char)(c))
#define isdigit(c) testref_isdigit((unsigned char)(c))
#define isalpha(c) testref_isalpha((unsigned char)(c))
#define isalnum(c) testref_isalnum((unsigned char)(c))
#define isprint(c) ((c) >= 0x20 && (c) <= 0x7e)
#define islower(c) testref_iscase((c), 1)
#define isupper(c) testref_iscase((c), 0)
#define tolower(c) testref_case((unsigned char)(c), 0x20)
#define toupper(c) testref_case((unsigned char)(c), 0)
#define iscntrl(c) testref_iscntrl((unsigned char)(c))
#define ispunct(c) testref_ispunct((unsigned char)(c))
#define isxdigit(c) testref_isxdigit((unsigned char)(c))

#define UNUSED __attribute__((unused))
#define signed_add_overflows(a, b) \
	((b) > 0 ? (a) > LONG_MAX - (b) : (a) < LONG_MIN - (b))

static inline __attribute__((noreturn)) void testref_die(const char *format, ...)
{
	va_list ap;
	va_start(ap, format);
	vfprintf(stderr, format, ap);
	va_end(ap);
	fputc('\n', stderr);
	exit(2);
}

#define BUG(...) testref_die(__VA_ARGS__)

static inline size_t st_add(size_t a, size_t b)
{
	if (b > SIZE_MAX - a)
		testref_die("size_t overflow: %zu + %zu", a, b);
	return a + b;
}

#define st_add3(a,b,c) st_add(st_add((a),(b)),(c))
#define st_add4(a,b,c,d) st_add(st_add3((a),(b),(c)),(d))
#define st_add5(a,b,c,d,e) st_add(st_add4((a),(b),(c),(d)),(e))

static inline void *xmalloc(size_t size)
{
	void *p = malloc(size ? size : 1);
	if (!p)
		testref_die("out of memory");
	return p;
}

static inline void *xcalloc(size_t count, size_t size)
{
	void *p;
	if (size && count > SIZE_MAX / size)
		testref_die("allocation overflow");
	p = calloc(count ? count : 1, size ? size : 1);
	if (!p)
		testref_die("out of memory");
	return p;
}

static inline void *xrealloc(void *old, size_t size)
{
	void *p = realloc(old, size ? size : 1);
	if (!p)
		testref_die("out of memory");
	return p;
}

static inline int regexec_buf(const regex_t *regex, const char *buf,
			     size_t size, size_t nmatch,
			     regmatch_t matches[], int flags)
{
	matches[0].rm_so = 0;
	matches[0].rm_eo = size;
	return regexec(regex, buf, nmatch, matches, flags | REG_STARTEND);
}

#endif

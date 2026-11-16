/*
 * anthy_top1 reads one reading per line from stdin and writes the top-1
 * conversion for that reading to stdout, one line per input line.
 *
 * It exists only for the T03 spike: it provides the "traditional" kana-kanji
 * baseline that the zenz numbers are compared against. Anthy state is reset
 * for every line and no segment is committed, so the user dictionary and
 * learning data are never updated.
 *
 * Build:
 *   cc -O2 -o anthy_top1 anthy_top1.c $(pkg-config --cflags --libs anthy)
 */
#include <anthy/anthy.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define BUFSZ 4096

static void die(const char *msg) {
	fprintf(stderr, "anthy_top1: %s\n", msg);
	exit(1);
}

int main(void) {
	char *line = NULL;
	size_t cap = 0;
	ssize_t len;
	anthy_context_t ctx;

	if (anthy_init() != 0)
		die("anthy_init failed");
	ctx = anthy_create_context();
	if (ctx == NULL)
		die("anthy_create_context failed");
	/* Returns the encoding now in effect, not an error code. */
	if (anthy_context_set_encoding(ctx, ANTHY_UTF8_ENCODING) != ANTHY_UTF8_ENCODING)
		die("anthy_context_set_encoding failed");

	while ((len = getline(&line, &cap, stdin)) != -1) {
		while (len > 0 && (line[len - 1] == '\n' || line[len - 1] == '\r'))
			line[--len] = '\0';
		if (len == 0) {
			putchar('\n');
			continue;
		}

		anthy_reset_context(ctx);
		if (anthy_set_string(ctx, line) != 0) {
			putchar('\n');
			continue;
		}

		struct anthy_conv_stat cs;
		if (anthy_get_stat(ctx, &cs) != 0) {
			putchar('\n');
			continue;
		}
		for (int i = 0; i < cs.nr_segment; i++) {
			char buf[BUFSZ];
			/* anthy_get_segment returns the byte length, or a negative error. */
			if (anthy_get_segment(ctx, i, 0, buf, sizeof(buf)) >= 0)
				fputs(buf, stdout);
		}
		putchar('\n');
		fflush(stdout);
	}

	free(line);
	anthy_release_context(ctx);
	anthy_quit();
	return 0;
}

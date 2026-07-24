package rynek

import "strings"

// Codec is a transparent compression scheme applied to task artifacts. The zero
// value NoCodec means plain, uncompressed files.
//
// A codec touches two places, both hidden from the task's Cmd:
//
//   - Reading: an input path is rendered by readExpr, which for a compressed
//     codec expands to a streaming process substitution. `< {in}`, `cat {in}`,
//     and `$(… {in})` therefore all see decompressed bytes with no change to
//     the pipeline.
//   - Writing: Shell runs the task to a plaintext temp, then promotes it through
//     the codec (see Shell.Run), so {out} needs no awareness of compression.
//
// The scheme is inferred back from the filename extension on read (see
// codecForPath), so a task transparently decompresses any compressed input --
// whether a sibling task or an external process produced it.
type Codec string

const (
	NoCodec Codec = ""
	Zstd    Codec = "zstd"
)

// suffix is the filename suffix a codec appends to an artifact path.
func (c Codec) suffix() string {
	switch c {
	case Zstd:
		return ".zst"
	default:
		return ""
	}
}

// compressExpr is the bash template that compresses the plaintext file {src}
// into {dst}. -T0 uses every core; -q keeps zstd's progress off stderr. It is
// only consulted for a compressed codec.
func (c Codec) compressExpr() string {
	switch c {
	case Zstd:
		return `zstd -q -T0 -c {src} > {dst}`
	default:
		return `cat {src} > {dst}`
	}
}

// codecForPath infers a codec from an artifact's extension.
func codecForPath(path string) Codec {
	if strings.HasSuffix(path, Zstd.suffix()) {
		return Zstd
	}
	return NoCodec
}

// readExpr renders a shell expression yielding the decompressed contents of
// path: a streaming process substitution for a compressed codec (no temp file,
// -T0 for throughput), or just the quoted path for a plain file. Either form is
// a drop-in wherever a filename appears in a Cmd.
func readExpr(path string) string {
	q := shellQuote(path)
	switch codecForPath(path) {
	case Zstd:
		return "<(zstd -q -T0 -dcf " + q + ")"
	default:
		return q
	}
}

// shellQuote single-quotes s so it is a safe single shell word even if the base
// directory contains spaces or metacharacters.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

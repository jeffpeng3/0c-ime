package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
)

// LoadJADecoder loads the T12 statistical converter from a T13-style data
// directory (data/ja or a compiled pack with lexicon/words.tsv and
// connection.tsv).  The evaluator does not train, tune, or learn.
func LoadJADecoder(dir string) (*decode.Decoder, JAEngineInfo, error) {
	words := filepath.Join(dir, "lexicon", "words.tsv")
	conns := filepath.Join(dir, "connection.tsv")
	info := JAEngineInfo{Name: "stats", Data: dir}

	wh, err := shaFile(words)
	if err != nil {
		return nil, info, err
	}
	ch, err := shaFile(conns)
	if err != nil {
		return nil, info, err
	}
	info.LexiconSHA256 = wh
	info.ConnectionSHA256 = ch

	wf, err := os.Open(words)
	if err != nil {
		return nil, info, err
	}
	defer wf.Close()
	lex, err := lexicon.Load(wf)
	if err != nil {
		return nil, info, fmt.Errorf("%s: %w", words, err)
	}

	cf, err := os.Open(conns)
	if err != nil {
		return nil, info, err
	}
	defer cf.Close()
	conn, err := lexicon.LoadConnections(cf)
	if err != nil {
		return nil, info, fmt.Errorf("%s: %w", conns, err)
	}

	return &decode.Decoder{Lex: lex, Conn: conn}, info, nil
}

func shaFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

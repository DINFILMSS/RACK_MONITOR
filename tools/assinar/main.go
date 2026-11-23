// Assina o SHA256SUMS.txt de uma versão com a chave ed25519 da atualização automática.
//
//	go run ./tools/assinar -gerar                  # cria um par de chaves novo
//	UPDATE_SIGNING_KEY=… go run ./tools/assinar dist/SHA256SUMS.txt
//
// Grava dist/SHA256SUMS.txt.sig (assinatura em base64). Os programas instalados
// só aceitam uma atualização se essa assinatura conferir com a chave pública
// embutida (updatePubKey em updater.go).
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-gerar" {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil { fail(err.Error()) }
		fmt.Println("CHAVE PÚBLICA (vai em updatePubKey, updater.go):")
		fmt.Println(base64.StdEncoding.EncodeToString(pub))
		fmt.Println()
		fmt.Println("CHAVE PRIVADA (secret UPDATE_SIGNING_KEY no GitHub — NUNCA no código):")
		fmt.Println(base64.StdEncoding.EncodeToString(priv.Seed()))
		return
	}
	if len(os.Args) != 2 { fail("uso: assinar ARQUIVO  (ou -gerar)") }
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("UPDATE_SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		fail("UPDATE_SIGNING_KEY ausente ou inválida (crie o secret no GitHub: Settings → Secrets and variables → Actions)")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil { fail(err.Error()) }
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
	if err := os.WriteFile(os.Args[1]+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil { fail(err.Error()) }
	fmt.Println("assinado:", os.Args[1]+".sig")
	fmt.Println("chave pública:", base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)))
}

func fail(s string) { fmt.Fprintln(os.Stderr, s); os.Exit(1) }

//go:build ignore

// mkrsrc: gera um objeto COFF (.syso) com o ícone do programa para o Windows.
// uso: go run tools/mkrsrc.go icone.ico saida.syso
package main

import (
	"bytes"
	"encoding/binary"
	"os"
)

type img struct {
	w, h, colors, planes, bpp byte
	planes16, bpp16           uint16
	data                      []byte
}

func main() {
	ico, _ := os.ReadFile(os.Args[1])
	n := int(binary.LittleEndian.Uint16(ico[4:]))
	var imgs []img
	for i := 0; i < n; i++ {
		e := ico[6+16*i:]
		size := binary.LittleEndian.Uint32(e[8:])
		off := binary.LittleEndian.Uint32(e[12:])
		imgs = append(imgs, img{w: e[0], h: e[1], colors: e[2], planes16: binary.LittleEndian.Uint16(e[4:]), bpp16: binary.LittleEndian.Uint16(e[6:]), data: ico[off : off+size]})
	}
	// grupo de ícones (RT_GROUP_ICON)
	var grp bytes.Buffer
	binary.Write(&grp, binary.LittleEndian, []uint16{0, 1, uint16(n)})
	for i, im := range imgs {
		grp.Write([]byte{im.w, im.h, im.colors, 0})
		binary.Write(&grp, binary.LittleEndian, im.planes16)
		binary.Write(&grp, binary.LittleEndian, im.bpp16)
		binary.Write(&grp, binary.LittleEndian, uint32(len(im.data)))
		binary.Write(&grp, binary.LittleEndian, uint16(i+1))
	}
	// árvore de recursos: raiz → tipo (3 ícone, 14 grupo) → id → idioma → dado
	type leaf struct{ data []byte }
	types := []struct {
		id    uint32
		items [][]byte
	}{{3, nil}, {14, [][]byte{grp.Bytes()}}}
	for _, im := range imgs { types[0].items = append(types[0].items, im.data) }

	var dir bytes.Buffer
	w32 := func(v uint32) { binary.Write(&dir, binary.LittleEndian, v) }
	w16 := func(v uint16) { binary.Write(&dir, binary.LittleEndian, v) }
	dirHdr := func(count int) { w32(0); w32(0); w16(0); w16(0); w16(0); w16(uint16(count)) }
	// tamanhos para calcular deslocamentos
	rootSize := 16 + 8*len(types)
	l2Off := make([]int, len(types))
	off := rootSize
	for i, t := range types { l2Off[i] = off; off += 16 + 8*len(t.items) }
	l3Off := [][]int{}
	for _, t := range types {
		var o []int
		for range t.items { o = append(o, off); off += 16 + 8 }
		l3Off = append(l3Off, o)
	}
	dataEntryOff := [][]int{}
	for _, t := range types {
		var o []int
		for range t.items { o = append(o, off); off += 16 }
		dataEntryOff = append(dataEntryOff, o)
	}
	blobOff := [][]int{}
	for _, t := range types {
		var o []int
		for _, it := range t.items { off = (off + 7) &^ 7; o = append(o, off); off += len(it) }
		blobOff = append(blobOff, o)
	}
	total := (off + 7) &^ 7
	// raiz
	dirHdr(len(types))
	for i, t := range types { w32(t.id); w32(uint32(l2Off[i]) | 0x80000000) }
	for i, t := range types {
		dirHdr(len(t.items))
		for j := range t.items { w32(uint32(j + 1)); w32(uint32(l3Off[i][j]) | 0x80000000) }
	}
	for i, t := range types {
		for j := range t.items { dirHdr(1); w32(0x0409); w32(uint32(dataEntryOff[i][j])) }
	}
	var relocs []uint32
	for i, t := range types {
		for j, it := range t.items {
			relocs = append(relocs, uint32(dir.Len()))
			w32(uint32(blobOff[i][j])); w32(uint32(len(it))); w32(0); w32(0)
		}
	}
	for i, t := range types {
		for j, it := range t.items {
			for dir.Len() < blobOff[i][j] { dir.WriteByte(0) }
			dir.Write(it)
		}
	}
	for dir.Len() < total { dir.WriteByte(0) }
	data := dir.Bytes()

	var o bytes.Buffer
	le := func(v any) { binary.Write(&o, binary.LittleEndian, v) }
	symOff := 20 + 40 + len(data) + 10*len(relocs)
	le(uint16(0x8664)); le(uint16(1)); le(uint32(0)); le(uint32(symOff)); le(uint32(1)); le(uint16(0)); le(uint16(0))
	o.Write([]byte(".rsrc\x00\x00\x00"))
	le(uint32(0)); le(uint32(0)); le(uint32(len(data))); le(uint32(60)); le(uint32(60 + len(data))); le(uint32(0)); le(uint16(len(relocs))); le(uint16(0)); le(uint32(0x40000040))
	o.Write(data)
	for _, r := range relocs { le(r); le(uint32(0)); le(uint16(3)) } // IMAGE_REL_AMD64_ADDR32NB → símbolo 0 (.rsrc)
	o.Write([]byte(".rsrc\x00\x00\x00")); le(uint32(0)); le(int16(1)); le(uint16(0)); o.WriteByte(3); o.WriteByte(0)
	le(uint32(4))
	os.WriteFile(os.Args[2], o.Bytes(), 0o644)
}

// Copyright 2011 The Go Authors. All rights reserved.
// The Go BSD license is in ../../licenses/go-http-sniff-LICENSE.
// This port preserves net/http/internal/sniff.go signature order and matching.
// Source: https://go.dev/src/net/http/internal/sniff.go

enum Signature {
    Html(&'static [u8]),
    Exact(&'static [u8], &'static str),
    Masked(&'static [u8], &'static [u8], bool, &'static str),
    Mp4,
    Text,
}
const SIGNATURES:&[Signature]=&[
Signature::Html(b"<!DOCTYPE HTML"),
Signature::Html(b"<HTML"),
Signature::Html(b"<HEAD"),
Signature::Html(b"<SCRIPT"),
Signature::Html(b"<IFRAME"),
Signature::Html(b"<H1"),
Signature::Html(b"<DIV"),
Signature::Html(b"<FONT"),
Signature::Html(b"<TABLE"),
Signature::Html(b"<A"),
Signature::Html(b"<STYLE"),
Signature::Html(b"<TITLE"),
Signature::Html(b"<B"),
Signature::Html(b"<BODY"),
Signature::Html(b"<BR"),
Signature::Html(b"<P"),
Signature::Html(b"<!--"),
Signature::Masked(b"<?xml",b"\xff\xff\xff\xff\xff",true,"text/xml; charset=utf-8"),
Signature::Exact(b"%PDF-","application/pdf"),
Signature::Exact(b"%!PS-Adobe-","application/postscript"),
Signature::Masked(b"\xfe\xff\x00\x00",b"\xff\xff\x00\x00",false,"text/plain; charset=utf-16be"),
Signature::Masked(b"\xff\xfe\x00\x00",b"\xff\xff\x00\x00",false,"text/plain; charset=utf-16le"),
Signature::Masked(b"\xef\xbb\xbf\x00",b"\xff\xff\xff\x00",false,"text/plain; charset=utf-8"),
Signature::Exact(b"\x00\x00\x01\x00","image/x-icon"),
Signature::Exact(b"\x00\x00\x02\x00","image/x-icon"),
Signature::Exact(b"BM","image/bmp"),
Signature::Exact(b"GIF87a","image/gif"),
Signature::Exact(b"GIF89a","image/gif"),
Signature::Masked(b"RIFF\x00\x00\x00\x00WEBPVP",b"\xff\xff\xff\xff\x00\x00\x00\x00\xff\xff\xff\xff\xff\xff",false,"image/webp"),
Signature::Exact(b"\x89PNG\x0d\x0a\x1a\x0a","image/png"),
Signature::Exact(b"\xff\xd8\xff","image/jpeg"),
Signature::Masked(b"FORM\x00\x00\x00\x00AIFF",b"\xff\xff\xff\xff\x00\x00\x00\x00\xff\xff\xff\xff",false,"audio/aiff"),
Signature::Masked(b"ID3",b"\xff\xff\xff",false,"audio/mpeg"),
Signature::Masked(b"OggS\x00",b"\xff\xff\xff\xff\xff",false,"application/ogg"),
Signature::Masked(b"MThd\x00\x00\x00\x06",b"\xff\xff\xff\xff\xff\xff\xff\xff",false,"audio/midi"),
Signature::Masked(b"RIFF\x00\x00\x00\x00AVI ",b"\xff\xff\xff\xff\x00\x00\x00\x00\xff\xff\xff\xff",false,"video/avi"),
Signature::Masked(b"RIFF\x00\x00\x00\x00WAVE",b"\xff\xff\xff\xff\x00\x00\x00\x00\xff\xff\xff\xff",false,"audio/wave"),
Signature::Mp4,
Signature::Exact(b"\x1aE\xdf\xa3","video/webm"),
Signature::Masked(b"\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00LP",b"\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\xff\xff",false,"application/vnd.ms-fontobject"),
Signature::Exact(b"\x00\x01\x00\x00","font/ttf"),
Signature::Exact(b"OTTO","font/otf"),
Signature::Exact(b"ttcf","font/collection"),
Signature::Exact(b"wOFF","font/woff"),
Signature::Exact(b"wOF2","font/woff2"),
Signature::Exact(b"\x1f\x8b\x08","application/x-gzip"),
Signature::Exact(b"PK\x03\x04","application/zip"),
Signature::Exact(b"Rar!\x1a\x07\x00","application/x-rar-compressed"),
Signature::Exact(b"Rar!\x1a\x07\x01\x00","application/x-rar-compressed"),
Signature::Exact(b"\x00asm","application/wasm"),
Signature::Text];
pub fn detect(bytes: &[u8]) -> &'static str {
    let bytes = &bytes[..bytes.len().min(512)];
    let first = bytes
        .iter()
        .position(|byte| ![9, 10, 12, 13, 32].contains(byte))
        .unwrap_or(bytes.len());
    for signature in SIGNATURES {
        match signature {
            Signature::Html(pattern) => {
                let input = &bytes[first..];
                if input.len() > pattern.len()
                    && pattern.iter().zip(input).all(|(pattern, byte)| {
                        if pattern.is_ascii_uppercase() {
                            *pattern == byte.to_ascii_uppercase()
                        } else {
                            pattern == byte
                        }
                    })
                    && [b' ', b'>'].contains(&input[pattern.len()])
                {
                    return "text/html; charset=utf-8";
                }
            }
            Signature::Exact(pattern, kind) => {
                if bytes.starts_with(pattern) {
                    return kind;
                }
            }
            Signature::Masked(pattern, mask, skip, kind) => {
                let input = if *skip { &bytes[first..] } else { bytes };
                if input.len() >= pattern.len()
                    && pattern
                        .iter()
                        .zip(mask.iter())
                        .zip(input.iter())
                        .all(|((pattern, mask), byte)| *byte & *mask == *pattern)
                {
                    return kind;
                }
            }
            Signature::Mp4 => {
                if bytes.len() >= 12 {
                    let size = u32::from_be_bytes(bytes[..4].try_into().unwrap()) as usize;
                    if size >= 8 && size <= bytes.len() && size % 4 == 0 && &bytes[4..8] == b"ftyp"
                    {
                        for offset in (8..size).step_by(4) {
                            if offset != 12 && &bytes[offset..offset + 3] == b"mp4" {
                                return "video/mp4";
                            }
                        }
                    }
                }
            }
            Signature::Text => {
                if !bytes[first..].iter().any(|byte| {
                    *byte <= 8
                        || *byte == 11
                        || (14..=26).contains(byte)
                        || (28..=31).contains(byte)
                }) {
                    return "text/plain; charset=utf-8";
                }
            }
        }
    }
    "application/octet-stream"
}

package hub

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

func backupPicture(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 32, 32))); err != nil {
		t.Fatal(err)
	}
	if err := protocol.ValidatePicture(b.Bytes()); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pictureArchive(t *testing.T, name string, data []byte, typ byte) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: name, Mode: 0o777, Size: int64(len(data)), Typeflag: typ}
	if typ == tar.TypeSymlink {
		hdr.Size = 0
		hdr.Linkname = "../outside"
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if typ != tar.TypeSymlink {
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestBackupRestoreProfilePictures(t *testing.T) {
	h, id, addr := testHub(t)
	owner := member{id, addr}
	_ = personOf(t, h, owner)
	bob := enroll(t, h, "bob")
	picture := backupPicture(t)
	hash := protocol.PictureHash(picture)
	if code, body := owner.call(t, h, "PUT", "/v1/pictures/"+hash, picture); code != 204 {
		t.Fatalf("picture: %d %s", code, body)
	}
	ciphertext := []byte("exact encrypted attachment bytes")
	blob := owner.upload(t, h, bob.addr, ciphertext)
	recipient, err := bob.id.Public(bob.addr).Recipient()
	if err != nil {
		t.Fatal(err)
	}
	in := envelope.Inner{ID: protocol.NewID(), From: addr, To: bob.addr, TS: 1, Kind: envelope.KindMessage, Body: "retained message", Attachments: []envelope.Attachment{{Name: "fixture.bin", Blob: envelope.Blob{ID: blob, Size: int64(len(ciphertext)), SHA256: digest(ciphertext)}}}}
	sealed, err := envelope.Seal(in, id.Sign, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if code, body := owner.call(t, h, "POST", "/v1/messages", sealed); code != 202 {
		t.Fatalf("message: %d %s", code, body)
	}
	dir := h.cfg.DataDir
	h.Close()
	m, err := OpenMaintenance(dir)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := m.Backup(&archive); err != nil {
		m.Close()
		t.Fatal(err)
	}
	m.Close()
	restored := filepath.Join(t.TempDir(), "restored")
	sum, err := Restore(&archive, restored)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Agents != 2 || sum.Messages != 1 || sum.Blobs != 1 {
		t.Fatalf("summary: %+v", sum)
	}
	for path, want := range map[string][]byte{"pictures/" + hash + ".png": picture, "blobs/" + blob + ".blob": ciphertext} {
		got, err := os.ReadFile(filepath.Join(restored, filepath.FromSlash(path)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s bytes: %v", path, err)
		}
	}
	if runtime.GOOS != "windows" {
		for path, mode := range map[string]os.FileMode{"": 0o700, "pictures": 0o700, "pictures/" + hash + ".png": 0o600, "hub.db": 0o600} {
			info, err := os.Stat(filepath.Join(restored, filepath.FromSlash(path)))
			if err != nil || info.Mode().Perm() != mode {
				t.Fatalf("%s permissions: %v %v", path, info, err)
			}
		}
	}
}

func TestRestoreRejectsDamagedPictures(t *testing.T) {
	good := backupPicture(t)
	for _, tc := range []struct {
		name string
		data []byte
		hash string
	}{
		{"wrong hash", good, strings.Repeat("0", 64)},
		{"not PNG", []byte("not a PNG"), protocol.PictureHash([]byte("not a PNG"))},
		{"truncated", good[:len(good)-1], protocol.PictureHash(good[:len(good)-1])},
		{"oversize", bytes.Repeat([]byte("x"), protocol.MaxPictureBytes+1), strings.Repeat("0", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := pictureArchive(t, "pictures/"+tc.hash+".png", tc.data, tar.TypeReg)
			if _, err := Restore(bytes.NewReader(archive), filepath.Join(t.TempDir(), "r")); err == nil || !strings.Contains(err.Error(), "picture") {
				t.Fatalf("damaged picture accepted or wrong failure: %v", err)
			}
		})
	}
}

func TestRestoreRejectsPicturePathsAndSymlinks(t *testing.T) {
	data := backupPicture(t)
	valid := "pictures/" + protocol.PictureHash(data) + ".png"
	for _, name := range []string{"pictures/../hub.db", "pictures/../../outside", "pictures/NOTHEX.png", "pictures/" + strings.Repeat("a", 63) + ".png", strings.ReplaceAll(valid, "/", `\`)} {
		t.Run(name, func(t *testing.T) {
			archive := pictureArchive(t, name, data, tar.TypeReg)
			if _, err := Restore(bytes.NewReader(archive), filepath.Join(t.TempDir(), "r")); err == nil || !strings.Contains(err.Error(), "not allowed") {
				t.Fatalf("unsafe picture entry: %v", err)
			}
		})
	}
	archive := pictureArchive(t, valid, nil, tar.TypeSymlink)
	if _, err := Restore(bytes.NewReader(archive), filepath.Join(t.TempDir(), "r")); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("archive symlink: %v", err)
	}
	outside := t.TempDir()
	link := filepath.Join(t.TempDir(), "restore")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	archive = pictureArchive(t, valid, data, tar.TypeReg)
	if _, err := Restore(bytes.NewReader(archive), link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("restore destination symlink: %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("restore wrote through destination symlink: %v %v", entries, err)
	}
}

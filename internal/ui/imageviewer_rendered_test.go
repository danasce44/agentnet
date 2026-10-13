package ui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"testing"
)

type imageViewerFixture struct {
	*imageOpenFixture
	images [][]byte
}

func (f *imageViewerFixture) DM(id string) (DMThread, error) {
	dm, err := f.pictureFixture.DM(id)
	dm.Messages[0].Attachments = []FileView{
		{Index: 2, Name: "wide.png", Size: int64(len(f.images[0])), Openable: true},
		{Index: 5, Name: "tall.png", Size: int64(len(f.images[1])), Openable: true},
	}
	return dm, err
}

func (f *imageViewerFixture) OpenFile(_ context.Context, dir, id string, index int) (io.ReadCloser, string, error) {
	if dir != "in" || id != "picture-message" {
		return nil, "", ErrNotFound
	}
	switch index {
	case 2:
		return io.NopCloser(bytes.NewReader(f.images[0])), "wide.png", nil
	case 5:
		return io.NopCloser(bytes.NewReader(f.images[1])), "tall.png", nil
	}
	return nil, "", ErrNotFound
}

func TestImageViewerRendered(t *testing.T) {
	f := &imageViewerFixture{imageOpenFixture: &imageOpenFixture{pictureFixture: newPictureFixture()}}
	for i, rect := range []image.Rectangle{image.Rect(0, 0, 2400, 1600), image.Rect(0, 0, 1000, 2000)} {
		img := image.NewNRGBA(rect)
		for y := 0; y < rect.Dy(); y++ {
			for x := 0; x < rect.Dx(); x++ {
				img.SetNRGBA(x, y, color.NRGBA{uint8(80 + 100*i), uint8(x % 256), uint8(y % 256), 255})
			}
		}
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		f.images = append(f.images, b.Bytes())
	}
	ts := httptest.NewUnstartedServer(nil)
	s := New(f, ts.Listener.Addr().String(), testToken)
	ts.Config.Handler = s.Handler()
	ts.Start()
	defer ts.Close()
	t.Log(browserCheck(t, "testdata/image_viewer_check.cjs", ts.URL+"/?t="+testToken))
	f.lock.Lock()
	defer f.lock.Unlock()
	if len(f.opened) != 2 || f.opened[0] != (imageReference{"in", "picture-message", 2}) || f.opened[1] != (imageReference{"in", "picture-message", 5}) {
		t.Fatal("desktop opened the wrong attachment", f.opened)
	}
}

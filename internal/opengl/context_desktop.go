// Copyright 2014 Hajime Hoshi
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build darwin freebsd linux windows
// +build !js
// +build !android
// +build !ios

package opengl

import (
	"errors"
	"fmt"

	"github.com/go-gl/gl/v2.1/gl"

	"github.com/hajimehoshi/ebiten/internal/graphics"
)

type (
	Texture     uint32
	Framebuffer uint32
	shader      uint32
	program     uint32
	buffer      uint32
)

var InvalidTexture Texture

type (
	uniformLocation int32
	attribLocation  int32
)

type programID uint32

const (
	invalidTexture     = 0
	invalidFramebuffer = (1 << 32) - 1
)

func getProgramID(p program) programID {
	return programID(p)
}

func init() {
	vertexShader = gl.VERTEX_SHADER
	fragmentShader = gl.FRAGMENT_SHADER
	arrayBuffer = gl.ARRAY_BUFFER
	elementArrayBuffer = gl.ELEMENT_ARRAY_BUFFER
	dynamicDraw = gl.DYNAMIC_DRAW
	Short = gl.SHORT
	Float = gl.FLOAT

	zero = gl.ZERO
	one = gl.ONE
	srcAlpha = gl.SRC_ALPHA
	dstAlpha = gl.DST_ALPHA
	oneMinusSrcAlpha = gl.ONE_MINUS_SRC_ALPHA
	oneMinusDstAlpha = gl.ONE_MINUS_DST_ALPHA

	initializeArrayBuferLayout()
}

type context struct {
	init            bool
	runOnMainThread func(func() error) error
}

func Init(runOnMainThread func(func() error) error) {
	c := &Context{}
	c.runOnMainThread = runOnMainThread
	theContext = c
}

func (c *Context) runOnContextThread(f func() error) error {
	return c.runOnMainThread(f)
}

func (c *Context) reset() error {
	if err := c.runOnContextThread(func() error {
		if c.init {
			return nil
		}
		// Note that this initialization must be done after Loop is called.
		if err := gl.Init(); err != nil {
			return fmt.Errorf("opengl: initializing error %v", err)
		}
		c.init = true
		return nil
	}); err != nil {
		return err
	}
	c.locationCache = newLocationCache()
	c.lastTexture = invalidTexture
	c.lastFramebuffer = invalidFramebuffer
	c.lastViewportWidth = 0
	c.lastViewportHeight = 0
	c.lastCompositeMode = graphics.CompositeModeUnknown
	_ = c.runOnContextThread(func() error {
		gl.Enable(gl.BLEND)
		return nil
	})
	c.BlendFunc(graphics.CompositeModeSourceOver)
	_ = c.runOnContextThread(func() error {
		f := int32(0)
		gl.GetIntegerv(gl.FRAMEBUFFER_BINDING, &f)
		c.screenFramebuffer = Framebuffer(f)
		return nil
	})
	return nil
}

func (c *Context) BlendFunc(mode graphics.CompositeMode) {
	_ = c.runOnContextThread(func() error {
		if c.lastCompositeMode == mode {
			return nil
		}
		c.lastCompositeMode = mode
		s, d := mode.Operations()
		s2, d2 := convertOperation(s), convertOperation(d)
		gl.BlendFunc(uint32(s2), uint32(d2))
		return nil
	})
}

func (c *Context) NewTexture(width, height int) (Texture, error) {
	var texture Texture
	if err := c.runOnContextThread(func() error {
		var t uint32
		gl.GenTextures(1, &t)
		// TODO: Use gl.IsTexture
		if t <= 0 {
			return errors.New("opengl: creating texture failed")
		}
		gl.PixelStorei(gl.UNPACK_ALIGNMENT, 4)
		texture = Texture(t)
		return nil
	}); err != nil {
		return 0, err
	}
	c.BindTexture(texture)
	_ = c.runOnContextThread(func() error {
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
		gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, int32(width), int32(height), 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
		return nil
	})
	return texture, nil
}

func (c *Context) bindFramebufferImpl(f Framebuffer) {
	_ = c.runOnContextThread(func() error {
		gl.BindFramebufferEXT(gl.FRAMEBUFFER, uint32(f))
		return nil
	})
}

func (c *Context) FramebufferPixels(f Framebuffer, width, height int) ([]byte, error) {
	var pixels []byte
	_ = c.runOnContextThread(func() error {
		gl.Flush()
		return nil
	})
	c.bindFramebuffer(f)
	if err := c.runOnContextThread(func() error {
		pixels = make([]byte, 4*width*height)
		gl.ReadPixels(0, 0, int32(width), int32(height), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pixels))
		if e := gl.GetError(); e != gl.NO_ERROR {
			pixels = nil
			return fmt.Errorf("opengl: glReadPixels: %d", e)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return pixels, nil
}

func (c *Context) bindTextureImpl(t Texture) {
	_ = c.runOnContextThread(func() error {
		gl.BindTexture(gl.TEXTURE_2D, uint32(t))
		return nil
	})
}

func (c *Context) DeleteTexture(t Texture) {
	_ = c.runOnContextThread(func() error {
		tt := uint32(t)
		if !gl.IsTexture(tt) {
			return nil
		}
		if c.lastTexture == t {
			c.lastTexture = invalidTexture
		}
		gl.DeleteTextures(1, &tt)
		return nil
	})
}

func (c *Context) IsTexture(t Texture) bool {
	r := false
	_ = c.runOnContextThread(func() error {
		r = gl.IsTexture(uint32(t))
		return nil
	})
	return r
}

func (c *Context) TexSubImage2D(p []byte, x, y, width, height int) {
	_ = c.runOnContextThread(func() error {
		gl.TexSubImage2D(gl.TEXTURE_2D, 0, int32(x), int32(y), int32(width), int32(height), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(p))
		return nil
	})
}

func (c *Context) BindScreenFramebuffer() {
	c.bindFramebuffer(c.screenFramebuffer)
}

func (c *Context) NewFramebuffer(texture Texture) (Framebuffer, error) {
	var framebuffer Framebuffer
	var f uint32
	if err := c.runOnContextThread(func() error {
		gl.GenFramebuffersEXT(1, &f)
		// TODO: Use gl.IsFramebuffer
		if f <= 0 {
			return errors.New("opengl: creating framebuffer failed: gl.IsFramebuffer returns false")
		}
		return nil
	}); err != nil {
		return 0, err
	}
	c.bindFramebuffer(Framebuffer(f))
	if err := c.runOnContextThread(func() error {
		gl.FramebufferTexture2DEXT(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, uint32(texture), 0)
		s := gl.CheckFramebufferStatusEXT(gl.FRAMEBUFFER)
		if s != gl.FRAMEBUFFER_COMPLETE {
			if s != 0 {
				return fmt.Errorf("opengl: creating framebuffer failed: %v", s)
			}
			if e := gl.GetError(); e != gl.NO_ERROR {
				return fmt.Errorf("opengl: creating framebuffer failed: (glGetError) %d", e)
			}
			return fmt.Errorf("opengl: creating framebuffer failed: unknown error")
		}
		framebuffer = Framebuffer(f)
		return nil
	}); err != nil {
		return 0, err
	}
	return framebuffer, nil
}

func (c *Context) setViewportImpl(width, height int) {
	_ = c.runOnContextThread(func() error {
		gl.Viewport(0, 0, int32(width), int32(height))
		return nil
	})
}

func (c *Context) DeleteFramebuffer(f Framebuffer) {
	_ = c.runOnContextThread(func() error {
		ff := uint32(f)
		if !gl.IsFramebufferEXT(ff) {
			return nil
		}
		if c.lastFramebuffer == f {
			c.lastFramebuffer = invalidFramebuffer
			c.lastViewportWidth = 0
			c.lastViewportHeight = 0
		}
		gl.DeleteFramebuffersEXT(1, &ff)
		return nil
	})
}

func (c *Context) newShader(shaderType shaderType, source string) (shader, error) {
	var sh shader
	if err := c.runOnContextThread(func() error {
		s := gl.CreateShader(uint32(shaderType))
		if s == 0 {
			return fmt.Errorf("opengl: glCreateShader failed: shader type: %d", shaderType)
		}
		cSources, free := gl.Strs(source + "\x00")
		gl.ShaderSource(uint32(s), 1, cSources, nil)
		free()
		gl.CompileShader(s)

		var v int32
		gl.GetShaderiv(s, gl.COMPILE_STATUS, &v)
		if v == gl.FALSE {
			log := []byte{}
			gl.GetShaderiv(uint32(s), gl.INFO_LOG_LENGTH, &v)
			if v != 0 {
				log = make([]byte, int(v))
				gl.GetShaderInfoLog(uint32(s), v, nil, (*uint8)(gl.Ptr(log)))
			}
			return fmt.Errorf("opengl: shader compile failed: %s", log)
		}
		sh = shader(s)
		return nil
	}); err != nil {
		return 0, err
	}
	return sh, nil
}

func (c *Context) deleteShader(s shader) {
	_ = c.runOnContextThread(func() error {
		gl.DeleteShader(uint32(s))
		return nil
	})
}

func (c *Context) newProgram(shaders []shader) (program, error) {
	var pr program
	if err := c.runOnContextThread(func() error {
		p := gl.CreateProgram()
		if p == 0 {
			return errors.New("opengl: glCreateProgram failed")
		}

		for _, shader := range shaders {
			gl.AttachShader(p, uint32(shader))
		}
		gl.LinkProgram(p)
		var v int32
		gl.GetProgramiv(p, gl.LINK_STATUS, &v)
		if v == gl.FALSE {
			return errors.New("opengl: program error")
		}
		pr = program(p)
		return nil
	}); err != nil {
		return 0, err
	}
	return pr, nil
}

func (c *Context) useProgram(p program) {
	_ = c.runOnContextThread(func() error {
		gl.UseProgram(uint32(p))
		return nil
	})
}

func (c *Context) deleteProgram(p program) {
	_ = c.runOnContextThread(func() error {
		if !gl.IsProgram(uint32(p)) {
			return nil
		}
		gl.DeleteProgram(uint32(p))
		return nil
	})
}

func (c *Context) getUniformLocationImpl(p program, location string) uniformLocation {
	l, free := gl.Strs(location + "\x00")
	uniform := uniformLocation(gl.GetUniformLocation(uint32(p), *l))
	free()
	if uniform == -1 {
		panic("opengl: invalid uniform location: " + location)
	}
	return uniform
}

func (c *Context) uniformInt(p program, location string, v int) {
	_ = c.runOnContextThread(func() error {
		l := int32(c.locationCache.GetUniformLocation(c, p, location))
		gl.Uniform1i(l, int32(v))
		return nil
	})
}

func (c *Context) uniformFloat(p program, location string, v float32) {
	_ = c.runOnContextThread(func() error {
		l := int32(c.locationCache.GetUniformLocation(c, p, location))
		gl.Uniform1f(l, v)
		return nil
	})
}

func (c *Context) uniformFloats(p program, location string, v []float32) {
	_ = c.runOnContextThread(func() error {
		l := int32(c.locationCache.GetUniformLocation(c, p, location))
		switch len(v) {
		case 2:
			gl.Uniform2fv(l, 1, (*float32)(gl.Ptr(v)))
		case 4:
			gl.Uniform4fv(l, 1, (*float32)(gl.Ptr(v)))
		case 16:
			gl.UniformMatrix4fv(l, 1, false, (*float32)(gl.Ptr(v)))
		default:
			panic("not reached")
		}
		return nil
	})
}

func (c *Context) getAttribLocationImpl(p program, location string) attribLocation {
	l, free := gl.Strs(location + "\x00")
	attrib := attribLocation(gl.GetAttribLocation(uint32(p), *l))
	free()
	if attrib == -1 {
		panic("opengl: invalid attrib location: " + location)
	}
	return attrib
}

func (c *Context) vertexAttribPointer(p program, location string, size int, dataType DataType, stride int, offset int) {
	_ = c.runOnContextThread(func() error {
		l := c.locationCache.GetAttribLocation(c, p, location)
		gl.VertexAttribPointer(uint32(l), int32(size), uint32(dataType), false, int32(stride), gl.PtrOffset(offset))
		return nil
	})
}

func (c *Context) enableVertexAttribArray(p program, location string) {
	_ = c.runOnContextThread(func() error {
		l := c.locationCache.GetAttribLocation(c, p, location)
		gl.EnableVertexAttribArray(uint32(l))
		return nil
	})
}

func (c *Context) disableVertexAttribArray(p program, location string) {
	_ = c.runOnContextThread(func() error {
		l := c.locationCache.GetAttribLocation(c, p, location)
		gl.DisableVertexAttribArray(uint32(l))
		return nil
	})
}

func (c *Context) newArrayBuffer(size int) buffer {
	var bf buffer
	_ = c.runOnContextThread(func() error {
		var b uint32
		gl.GenBuffers(1, &b)
		gl.BindBuffer(uint32(arrayBuffer), b)
		gl.BufferData(uint32(arrayBuffer), size, nil, uint32(dynamicDraw))
		bf = buffer(b)
		return nil
	})
	return bf
}

func (c *Context) newElementArrayBuffer(size int) buffer {
	var bf buffer
	_ = c.runOnContextThread(func() error {
		var b uint32
		gl.GenBuffers(1, &b)
		gl.BindBuffer(uint32(elementArrayBuffer), b)
		gl.BufferData(uint32(elementArrayBuffer), size, nil, uint32(dynamicDraw))
		bf = buffer(b)
		return nil
	})
	return bf
}

func (c *Context) bindBuffer(bufferType bufferType, b buffer) {
	_ = c.runOnContextThread(func() error {
		gl.BindBuffer(uint32(bufferType), uint32(b))
		return nil
	})
}

func (c *Context) ArrayBufferSubData(data []float32) {
	_ = c.runOnContextThread(func() error {
		gl.BufferSubData(uint32(arrayBuffer), 0, len(data)*4, gl.Ptr(data))
		return nil
	})
}

func (c *Context) ElementArrayBufferSubData(data []uint16) {
	_ = c.runOnContextThread(func() error {
		gl.BufferSubData(uint32(elementArrayBuffer), 0, len(data)*2, gl.Ptr(data))
		return nil
	})
}

func (c *Context) deleteBuffer(b buffer) {
	_ = c.runOnContextThread(func() error {
		bb := uint32(b)
		gl.DeleteBuffers(1, &bb)
		return nil
	})
}

func (c *Context) DrawElements(len int, offsetInBytes int) {
	_ = c.runOnContextThread(func() error {
		gl.DrawElements(gl.TRIANGLES, int32(len), gl.UNSIGNED_SHORT, gl.PtrOffset(offsetInBytes))
		return nil
	})
}

func (c *Context) maxTextureSizeImpl() int {
	size := 0
	_ = c.runOnContextThread(func() error {
		s := int32(0)
		gl.GetIntegerv(gl.MAX_TEXTURE_SIZE, &s)
		size = int(s)
		return nil
	})
	return size
}

func (c *Context) Flush() {
	_ = c.runOnContextThread(func() error {
		gl.Flush()
		return nil
	})
}

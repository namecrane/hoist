package hoist

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Client tests", func() {
	It("Should parse a path as a parent and child", func() {
		path, sub := ParsePath("/some/full/path")

		Expect(path).To(Equal("/some/full"))
		Expect(sub).To(Equal("path"))
	})
	It("Should parse a path as a parent and child with trailing slash", func() {
		path, sub := ParsePath("/some/full/path/")

		Expect(path).To(Equal("/some/full"))
		Expect(sub).To(Equal("path"))
	})
	It("Should parse a single level path", func() {
		path, sub := ParsePath("/something")

		Expect(path).To(Equal("/"))
		Expect(sub).To(Equal("something"))
	})
})

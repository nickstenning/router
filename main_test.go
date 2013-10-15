package main

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"os"
	"testing"
)

func TestMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "main")
}

var _ = Describe("main", func() {
	Describe("getenvDefault", func() {
		BeforeEach(func() {
			os.Clearenv()
		})

		Context("environment variable set", func() {
			BeforeEach(func() {
				os.Setenv("FOO", "environment value")
			})

			It("should use environment value", func() {
				Expect(getenvDefault("FOO", "default value")).To(Equal("environment value"))
			})
		})

		Context("environment variable set", func() {
			It("should use default", func() {
				Expect(getenvDefault("FOO", "default value")).To(Equal("default value"))
			})
		})
	})
})

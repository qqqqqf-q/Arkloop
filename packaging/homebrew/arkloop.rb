class Arkloop < Formula
  desc "Command-line client for Arkloop"
  homepage "https://github.com/qqqqqf-q/Arkloop"
  version "VERSION_PLACEHOLDER"
  license :cannot_represent

  RELEASE_TAG = "TAG_PLACEHOLDER"

  on_macos do
    on_arm do
      url "URL_DARWIN_ARM64"
      sha256 "SHA256_DARWIN_ARM64"
    end
    on_intel do
      url "URL_DARWIN_AMD64"
      sha256 "SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    on_arm do
      url "URL_LINUX_ARM64"
      sha256 "SHA256_LINUX_ARM64"
    end
    on_intel do
      url "URL_LINUX_AMD64"
      sha256 "SHA256_LINUX_AMD64"
    end
  end

  def install
    bin.install "ark"
    prefix.install "web"
    prefix.install "src"
  end

  test do
    assert_match "usage: ark <command> [flags]", shell_output("#{bin}/ark 2>&1", 2)
    assert_match "ark version #{RELEASE_TAG}", shell_output("#{bin}/ark version")
  end
end

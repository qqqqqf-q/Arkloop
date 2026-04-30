class Arkloop < Formula
  desc "Command-line client for Arkloop"
  homepage "https://github.com/qqqqqf-q/Arkloop"
  url "https://github.com/qqqqqf-q/Arkloop/archive/refs/tags/v26.4.29.tar.gz"
  sha256 "d9cb78d1096ed421d1a43c3a72ad983ccc5da93781cf8335a86984bb466b53f2"
  license :cannot_represent

  depends_on "go" => :build

  def install
    cd "src/services/cli" do
      system "go", "build", "-ldflags", "-X main.version=v#{version}", "-o", bin/"ark", "./cmd/ark"
    end
  end

  test do
    output = shell_output("#{bin}/ark 2>&1", 2)
    assert_match "usage: ark <command> [flags]", output
    assert_match "sessions resume <session-id>", output
  end
end

class Arkloop < Formula
  desc "Command-line client for Arkloop"
  homepage "https://github.com/qqqqqf-q/Arkloop"
  url "https://github.com/qqqqqf-q/Arkloop.git", tag: "v26.5.4"
  version "26.5.4"
  license :cannot_represent

  depends_on "go" => :build
  depends_on "node" => :build
  depends_on "pnpm" => :build

  def install
    system "pnpm", "install", "--frozen-lockfile"
    system "pnpm", "--dir", "src/apps/web", "build"
    prefix.install "src/apps/web/dist" => "web"

    cd "src/services/cli" do
      system "go", "build", "-tags", "desktop",
             "-ldflags", "-X main.version=v#{version} -X main.webRootHint=#{prefix}/web",
             "-o", bin/"ark", "./cmd/ark"
    end
  end

  test do
    output = shell_output("#{bin}/ark 2>&1", 2)
    assert_match "usage: ark <command> [flags]", output
    assert_match "sessions resume <session-id>", output

    assert_match "ark version v#{version}", shell_output("#{bin}/ark version")

    web_help = shell_output("#{bin}/ark web -h 2>&1", 1)
    assert_match "Usage of web:", web_help
    assert_match "-no-open", web_help
  end
end

# setup HOMEBREW_GITHUB_API_TOKEN from https://github.com/settings/tokens with *repo* scope (Full control of private repositories)
class DividatDriver < Formula
  desc ""
  homepage "https://dividat.com"
  url "https://github.com/dividat/driver-go/archive/develop.zip", :using => GitHubPrivateRepositoryDownloadStrategy
  version "0.1.1"
  # sha256 "fa6e97882146bf21104734f967bf5a4be93f021eca8ded6af410da64a02cd991"

  depends_on "go" => :build
  depends_on "glide" => :build
  depends_on "upx" => :build

  def install
    ENV["GOPATH"] = buildpath
    cd "src" do
      system "glide", "install"
    end
    system "make"
    bin.install "release/dividat-driver"
  end

  test do
    system "false"
  end
end

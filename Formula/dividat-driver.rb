class DividatDriver < Formula
  desc "Dividat Driver"
  homepage "https://dividat.com"
  url "https://github.com/dividat/driver.git", :tag => "2.1.0"
  head "https://github.com/dividat/driver.git", :branch => "develop"

  depends_on "go" => :build
  depends_on "dep" => :build

  def install
    ENV["GOPATH"] = buildpath
    cd "src/dividat-driver" do
      system "dep", "ensure"
    end
    system "make"
    bin.install "bin/dividat-driver"
  end

end

log_level = "TRACE"

plugin "triton-driver" {
  config {
    enabled = true
  }
}

client {
  options = {
    "driver.whitelist" = "triton"
  }
}

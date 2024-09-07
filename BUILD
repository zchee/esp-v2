filegroup(
    name = "version",
    srcs = ["VERSION"],
    visibility = ["//:__subpackages__"],
)

load("@hedron_compile_commands//:refresh_compile_commands.bzl", "refresh_compile_commands")

refresh_compile_commands(
    name = "refresh_compile_commands",
    targets = {
        # "//src/...": "",
        "//src/api_proxy/...": "",
        "//src/envoy/...": "",
    },
    exclude_external_sources = False,
    # exclude_headers = "all",  # not using "all" adds headers as sources to compile_commands.json which is never what we want
    max_threads = 32,
)

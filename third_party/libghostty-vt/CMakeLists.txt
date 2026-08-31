cmake_minimum_required(VERSION 3.19)
project(libghostty-vt-build LANGUAGES C)

# Pin ghostty to a specific commit. Bump this SHA to pick up upstream fixes.
# 15484b607eb5a518dedf1548247c923b8abaae7c (1.3.2-dev) requires zig 0.16.0,
# which is also the first zig able to link on macOS 26 (Tahoe).
set(GHOSTTY_GIT_TAG 15484b607eb5a518dedf1548247c923b8abaae7c)

include(FetchContent)
FetchContent_Declare(ghostty
    GIT_REPOSITORY https://github.com/ghostty-org/ghostty.git
    GIT_TAG ${GHOSTTY_GIT_TAG}
)
FetchContent_MakeAvailable(ghostty)

cmake_minimum_required(VERSION 3.19)
project(libghostty-vt-build LANGUAGES C)

# Pin ghostty to a specific commit. Bump this SHA to pick up upstream fixes.
# 43a05dc968eda9bfa2196d66ba1819daf510b62a drops libc++/libc++abi dependencies
# so libghostty-vt now only needs libc.
set(GHOSTTY_GIT_TAG 43a05dc968eda9bfa2196d66ba1819daf510b62a)

include(FetchContent)
FetchContent_Declare(ghostty
    GIT_REPOSITORY https://github.com/ghostty-org/ghostty.git
    GIT_TAG ${GHOSTTY_GIT_TAG}
)
FetchContent_MakeAvailable(ghostty)

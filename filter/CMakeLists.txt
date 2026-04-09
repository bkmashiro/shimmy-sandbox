cmake_minimum_required(VERSION 3.14)
project(shimmy_filter C)

find_package(DynamoRIO REQUIRED)

add_library(shimmy_filter SHARED shimmy_filter.c)
configure_DynamoRIO_client(shimmy_filter)
use_DynamoRIO_extension(shimmy_filter drsyscall)
use_DynamoRIO_extension(shimmy_filter drmgr)

# Silence implicit-function-declaration warnings for syscall numbers
# that the DynamoRIO headers may not fully declare on all toolchains.
target_compile_options(shimmy_filter PRIVATE -Wno-implicit-function-declaration)

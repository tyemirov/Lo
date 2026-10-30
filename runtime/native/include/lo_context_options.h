#ifndef LO_CONTEXT_OPTIONS_H
#define LO_CONTEXT_OPTIONS_H
typedef struct {
    __UINT64_TYPE__ steps;
    __UINT64_TYPE__ value_bytes;
    __UINT64_TYPE__ live_bytes;
    __UINT64_TYPE__ data_bytes;
    __UINT64_TYPE__ list_items;
    __UINT64_TYPE__ file_entries;
    __UINT64_TYPE__ file_directories;
} ContextOptions;
#endif

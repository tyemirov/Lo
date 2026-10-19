/* Compile/link contract only. The reviewed runner never executes this image. */
extern int lo_normalize_inherited_rights(void);

int main(void) {
    return lo_normalize_inherited_rights();
}

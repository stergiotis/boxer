cd 
cd repo/imzero_client_cpp
cd scripts/
./install_nonportable.sh 

sudo pacman -Syu gtk3
pactree gtk3 -l -u | sudo xargs pacman --noconfirm -Syu
cargo install puffin_viewer
puffin_viewer 

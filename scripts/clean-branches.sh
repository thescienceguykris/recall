git branch -d $(git branch --merged=main | grep -v main)
git fetch --prune